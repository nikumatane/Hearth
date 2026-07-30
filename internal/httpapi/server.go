package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"hearth/internal/buildinfo"
	"hearth/internal/config"
	"hearth/internal/panel"
	panelweb "hearth/web"
)

type server struct {
	config   config.Config
	service  panel.Service
	sessions *sessionStore
	logins   *loginGate
	access   *accessStore
}

func New(cfg config.Config, service panel.Service) (http.Handler, error) {
	access, err := newAccessStore(cfg.AdminPassword, cfg.AccessFile, cfg.AuditFile)
	if err != nil {
		return nil, err
	}
	s := &server{
		config:   cfg,
		service:  service,
		sessions: newSessionStore(),
		logins:   newLoginGate(10, time.Minute),
		access:   access,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", s.health)
	mux.HandleFunc("POST /api/v1/session", s.login)
	mux.HandleFunc("GET /api/v1/session", s.currentSession)
	mux.HandleFunc("DELETE /api/v1/session", s.logout)
	mux.HandleFunc("GET /api/v1/overview", s.auth(s.overview))
	mux.HandleFunc("GET /api/v1/games/{id}", s.auth(s.game))
	mux.HandleFunc("POST /api/v1/games/{id}/actions", s.auth(s.gameAction))
	mux.HandleFunc(
		"GET /api/v1/games/palworld/settings",
		s.permitted(permissionPalworldSettings, s.palworldSettings),
	)
	mux.HandleFunc(
		"PATCH /api/v1/games/palworld/settings",
		s.permitted(permissionPalworldSettings, s.updatePalworldSettings),
	)
	mux.HandleFunc(
		"GET /api/v1/games/palworld/world-option",
		s.permitted(permissionPalworldSettings, s.worldOption),
	)
	mux.HandleFunc(
		"PUT /api/v1/games/palworld/world-option",
		s.permitted(permissionPalworldSettings, s.updateWorldOption),
	)
	mux.HandleFunc("GET /api/v1/logs", s.admin(s.logs))
	mux.HandleFunc("GET /api/v1/access/members", s.admin(s.members))
	mux.HandleFunc("POST /api/v1/access/members", s.admin(s.createMember))
	mux.HandleFunc("PATCH /api/v1/access/members/{id}", s.admin(s.updateMember))
	mux.HandleFunc("DELETE /api/v1/access/members/{id}", s.admin(s.deleteMember))
	mux.HandleFunc("GET /api/v1/access/audit", s.admin(s.loginAudit))
	mux.Handle("/", spaHandler())
	return securityHeaders(mux), nil
}

func (s *server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok", "version": buildinfo.Version,
	})
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	ip := requestIP(r)
	now := time.Now()
	if allowed, retryAfter := s.logins.allow(time.Now()); !allowed {
		s.access.recordAudit(auditEntry{
			ID: newAuditID(), IP: ip, CredentialID: "RATE-LIMITED",
			Success: false, Reason: "登录尝试过多", CreatedAt: now,
		})
		w.Header().Set("Retry-After", retryAfterHeader(retryAfter))
		writeError(w, http.StatusTooManyRequests, "登录尝试过多，请稍后再试")
		return
	}
	var request struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &request); err != nil {
		s.access.recordAudit(auditEntry{
			ID: newAuditID(), IP: ip, CredentialID: "INVALID-REQUEST",
			Success: false, Reason: "请求格式不正确", CreatedAt: now,
		})
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	identity, authenticated := s.access.authenticate(request.Password)
	request.Password = ""
	if !authenticated {
		s.logins.recordFailure(time.Now())
		s.access.recordAudit(auditEntry{
			ID: newAuditID(), IP: ip, CredentialID: "UNMATCHED",
			Success: false, Reason: "密码不匹配", CreatedAt: now,
		})
		time.Sleep(250 * time.Millisecond)
		writeError(w, http.StatusUnauthorized, "密码不正确")
		return
	}
	s.logins.reset()
	token, err := s.sessions.create(identity)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法创建会话")
		return
	}
	s.access.recordAudit(auditEntry{
		ID: newAuditID(), IP: ip, CredentialID: identity.CredentialID,
		Role: identity.Role, Success: true, CreatedAt: now,
	})
	http.SetCookie(w, &http.Cookie{
		Name: "hearth_session", Value: token, Path: "/", HttpOnly: true,
		Secure: s.secureCookie(r), SameSite: http.SameSiteStrictMode,
	})
	name := "管理员"
	if identity.Role == roleMember {
		name = "成员 " + identity.CredentialID
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true, "name": name, "role": identity.Role,
		"credentialId": identity.CredentialID, "permissions": identity.Permissions,
		"version": buildinfo.Version,
	})
}

func (s *server) currentSession(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("hearth_session")
	identity, valid := principal{}, false
	if err == nil {
		identity, valid = s.sessions.get(cookie.Value)
	}
	if !valid {
		writeJSON(w, http.StatusOK, map[string]any{
			"authenticated": false, "version": buildinfo.Version,
		})
		return
	}
	name := "管理员"
	if identity.Role == roleMember {
		name = "成员 " + identity.CredentialID
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true, "name": name, "role": identity.Role,
		"credentialId": identity.CredentialID, "permissions": identity.Permissions,
		"version": buildinfo.Version,
	})
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("hearth_session"); err == nil {
		s.sessions.delete(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: "hearth_session", Value: "", Path: "/", HttpOnly: true,
		Secure: s.secureCookie(r), SameSite: http.SameSiteStrictMode, MaxAge: -1,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) overview(w http.ResponseWriter, r *http.Request) {
	overview := s.service.Overview()
	if identity, ok := principalFromContext(r.Context()); ok && identity.Role != roleAdmin {
		overview.Activities = []panel.Activity{}
	}
	writeJSON(w, http.StatusOK, overview)
}

func (s *server) game(w http.ResponseWriter, r *http.Request) {
	game, err := s.service.Game(r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, game)
}

func (s *server) gameAction(w http.ResponseWriter, r *http.Request) {
	var request panel.ActionRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	permission, ok := permissionForAction(request.Action)
	if !ok {
		writeServiceError(w, panel.ErrBadAction)
		return
	}
	identity, _ := principalFromContext(r.Context())
	if !hasPermission(identity, permission) {
		writeError(w, http.StatusForbidden, "当前成员密码没有执行此操作的权限")
		return
	}
	activity, err := s.service.RunAction(r.PathValue("id"), request.Action)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, activity)
}

func permissionForAction(action string) (string, bool) {
	switch action {
	case "start", "stop", "restart":
		return permissionGameControl, true
	case "update":
		return permissionGameUpdate, true
	case "backup":
		return permissionGameBackup, true
	default:
		return "", false
	}
}

func (s *server) palworldSettings(w http.ResponseWriter, _ *http.Request) {
	settings, err := s.service.PalworldSettings()
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *server) updatePalworldSettings(w http.ResponseWriter, r *http.Request) {
	var patch panel.PalworldSettingsPatch
	if err := decodeJSON(r, &patch); err != nil {
		writeError(w, http.StatusBadRequest, "配置增量格式不正确")
		return
	}
	updated, err := s.service.UpdatePalworldSettings(patch)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *server) worldOption(w http.ResponseWriter, _ *http.Request) {
	document, err := s.service.WorldOption()
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, document)
}

func (s *server) updateWorldOption(w http.ResponseWriter, r *http.Request) {
	var document panel.WorldOptionDocument
	if err := decodeJSONLimit(r, &document, 4<<20); err != nil {
		writeError(w, http.StatusBadRequest, "WorldOption.sav 请求格式不正确")
		return
	}
	updated, err := s.service.UpdateWorldOption(document)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *server) logs(w http.ResponseWriter, _ *http.Request) {
	files := make([]panel.LogFile, 0, 10)
	if s.config.LogFile != "" {
		if log, err := readLogTail("panel", "面板运行日志", s.config.LogFile); err == nil {
			files = append(files, log)
		}
	}
	logDirectory := filepath.Join(s.config.Games.Palworld.InstallDir, "panel-logs")
	entries, _ := os.ReadDir(logDirectory)
	type candidate struct {
		name string
		info fs.FileInfo
	}
	candidates := make([]candidate, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".log" {
			continue
		}
		info, err := entry.Info()
		if err == nil {
			candidates = append(candidates, candidate{name: entry.Name(), info: info})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].info.ModTime().After(candidates[j].info.ModTime())
	})
	if len(candidates) > 8 {
		candidates = candidates[:8]
	}
	for _, item := range candidates {
		label := "帕鲁启动日志"
		if strings.HasPrefix(item.name, "steamcmd-") {
			label = "SteamCMD 更新日志"
		}
		log, err := readLogTail(item.name, label+" · "+item.name, filepath.Join(logDirectory, item.name))
		if err == nil {
			files = append(files, log)
		}
	}
	writeJSON(w, http.StatusOK, panel.Logs{
		Activities: s.service.Overview().Activities,
		Files:      files,
	})
}

func readLogTail(id, label, path string) (panel.LogFile, error) {
	const maxBytes int64 = 128 << 10
	file, err := os.Open(path)
	if err != nil {
		return panel.LogFile{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return panel.LogFile{}, err
	}
	start := max(int64(0), info.Size()-maxBytes)
	buffer := make([]byte, info.Size()-start)
	if _, err := file.ReadAt(buffer, start); err != nil && !errors.Is(err, io.EOF) {
		return panel.LogFile{}, err
	}
	if start > 0 {
		if newline := strings.IndexByte(string(buffer), '\n'); newline >= 0 {
			buffer = buffer[newline+1:]
		}
	}
	return panel.LogFile{
		ID: id, Label: label, UpdatedAt: info.ModTime(),
		Content: string(buffer), Truncated: start > 0,
	}, nil
}

func (s *server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("hearth_session")
		identity, valid := principal{}, false
		if err == nil {
			identity, valid = s.sessions.get(cookie.Value)
		}
		if !valid {
			writeError(w, http.StatusUnauthorized, "需要登录")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), principalContextKey{}, identity)))
	}
}

func (s *server) permitted(permission string, next http.HandlerFunc) http.HandlerFunc {
	return s.auth(func(w http.ResponseWriter, r *http.Request) {
		identity, ok := principalFromContext(r.Context())
		if !ok || !hasPermission(identity, permission) {
			writeError(w, http.StatusForbidden, "当前成员密码没有访问此功能的权限")
			return
		}
		next(w, r)
	})
}

func (s *server) admin(next http.HandlerFunc) http.HandlerFunc {
	return s.auth(func(w http.ResponseWriter, r *http.Request) {
		identity, ok := principalFromContext(r.Context())
		if !ok || identity.Role != roleAdmin {
			writeError(w, http.StatusForbidden, "仅管理员可以访问")
			return
		}
		next(w, r)
	})
}

type principalContextKey struct{}

func principalFromContext(ctx context.Context) (principal, bool) {
	identity, ok := ctx.Value(principalContextKey{}).(principal)
	return identity, ok
}

func (s *server) secureCookie(r *http.Request) bool {
	if s.config.SecureCookies || r.TLS != nil {
		return true
	}
	proto := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])
	return strings.EqualFold(proto, "https")
}

func spaHandler() http.Handler {
	content, err := fs.Sub(panelweb.Assets, "dist")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(content))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if _, err := fs.Stat(content, path); err == nil {
				files.ServeHTTP(w, r)
				return
			}
		}
		r.URL.Path = "/"
		files.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set(
			"Content-Security-Policy",
			"default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'",
		)
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/"):
			w.Header().Set("Cache-Control", "no-store")
		case strings.HasPrefix(r.URL.Path, "/assets/"):
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		default:
			w.Header().Set("Cache-Control", "no-store, max-age=0")
		}
		next.ServeHTTP(w, r)
	})
}

func decodeJSON(r *http.Request, target any) error {
	return decodeJSONLimit(r, target, 1<<20)
}

func decodeJSONLimit(r *http.Request, target any, limit int64) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return errors.New("content type must be application/json")
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, limit))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, panel.ErrNotFound):
		writeError(w, http.StatusNotFound, "资源不存在")
	case errors.Is(err, panel.ErrBusy):
		writeError(w, http.StatusConflict, "该服务器已有任务正在执行")
	case errors.Is(err, panel.ErrBadAction):
		writeError(w, http.StatusBadRequest, "不支持的操作")
	case errors.Is(err, panel.ErrUnsafe):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, panel.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "操作失败")
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]session
}

type session struct {
	Principal principal
	ExpiresAt time.Time
}

func newSessionStore() *sessionStore {
	return &sessionStore{sessions: make(map[string]session)}
}

func (s *sessionStore) create(identity principal) (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(buffer)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for key, item := range s.sessions {
		if item.ExpiresAt.Before(now) {
			delete(s.sessions, key)
		}
	}
	s.sessions[token] = session{Principal: identity, ExpiresAt: now.Add(12 * time.Hour)}
	return token, nil
}

func (s *sessionStore) get(token string) (principal, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.sessions[token]
	if !ok || item.ExpiresAt.Before(time.Now()) {
		delete(s.sessions, token)
		return principal{}, false
	}
	return item.Principal, true
}

func (s *sessionStore) delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

func (s *sessionStore) deleteCredential(credentialID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for token, item := range s.sessions {
		if item.Principal.CredentialID == credentialID {
			delete(s.sessions, token)
		}
	}
}

type loginGate struct {
	mu          sync.Mutex
	maxFailures int
	window      time.Duration
	windowStart time.Time
	failures    int
}

func newLoginGate(maxFailures int, window time.Duration) *loginGate {
	return &loginGate{maxFailures: maxFailures, window: window}
}

func (g *loginGate) allow(now time.Time) (bool, time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.rollWindow(now)
	if g.failures < g.maxFailures {
		return true, 0
	}
	return false, maxDuration(time.Second, g.window-now.Sub(g.windowStart))
}

func (g *loginGate) recordFailure(now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.rollWindow(now)
	g.failures++
}

func (g *loginGate) reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.windowStart = time.Time{}
	g.failures = 0
}

func (g *loginGate) rollWindow(now time.Time) {
	if g.windowStart.IsZero() || now.Sub(g.windowStart) >= g.window {
		g.windowStart = now
		g.failures = 0
	}
}

func retryAfterHeader(duration time.Duration) string {
	seconds := int64(duration.Round(time.Second) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return fmt.Sprintf("%d", seconds)
}

func maxDuration(left, right time.Duration) time.Duration {
	if left > right {
		return left
	}
	return right
}
