package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
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
	config          config.Config
	service         panel.Service
	sessions        *sessionStore
	logins          *loginGate
	access          *accessStore
	configAudits    *configAuditStore
	operationAudits *operationAuditStore
	proxy           *proxyResolver
	devices         *deviceCookieManager
	ipRules         *ipRuleStore
}

func New(cfg config.Config, service panel.Service) (http.Handler, error) {
	access, err := newAccessStore(cfg.AdminPassword, cfg.AccessFile, cfg.AuditFile)
	if err != nil {
		return nil, err
	}
	configAudits, err := newConfigAuditStore(cfg.ConfigAuditFile)
	if err != nil {
		return nil, err
	}
	operationAudits, err := newOperationAuditStore(cfg.OperationAuditFile)
	if err != nil {
		return nil, err
	}
	proxy, err := newProxyResolver(cfg.TrustedProxyCIDRs)
	if err != nil {
		return nil, err
	}
	devices, err := newDeviceCookieManager(cfg.DeviceKeyFile)
	if err != nil {
		return nil, err
	}
	ipRules, err := newIPRuleStore(cfg.IPRulesFile)
	if err != nil {
		return nil, err
	}
	s := &server{
		config:          cfg,
		service:         service,
		sessions:        newSessionStore(),
		logins:          newLoginGate(),
		access:          access,
		configAudits:    configAudits,
		operationAudits: operationAudits,
		proxy:           proxy,
		devices:         devices,
		ipRules:         ipRules,
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
		s.permitted(permissionPalworldGameplay, s.palworldSettings),
	)
	mux.HandleFunc(
		"PATCH /api/v1/games/palworld/settings",
		s.permitted(permissionPalworldGameplay, s.updatePalworldSettings),
	)
	mux.HandleFunc(
		"GET /api/v1/games/palworld/world-option",
		s.admin(s.worldOption),
	)
	mux.HandleFunc(
		"PUT /api/v1/games/palworld/world-option",
		s.admin(s.updateWorldOption),
	)
	mux.HandleFunc("GET /api/v1/logs", s.admin(s.logs))
	mux.HandleFunc("GET /api/v1/access/members", s.admin(s.members))
	mux.HandleFunc("POST /api/v1/access/members", s.admin(s.createMember))
	mux.HandleFunc("PATCH /api/v1/access/members/{id}", s.admin(s.updateMember))
	mux.HandleFunc("DELETE /api/v1/access/members/{id}", s.admin(s.deleteMember))
	mux.HandleFunc("GET /api/v1/access/audit", s.admin(s.loginAudit))
	mux.HandleFunc("GET /api/v1/access/config-audit", s.admin(s.configAudit))
	mux.HandleFunc("GET /api/v1/access/operation-audit", s.admin(s.operationAudit))
	mux.HandleFunc("GET /api/v1/access/ip-rules", s.admin(s.listIPRules))
	mux.HandleFunc("POST /api/v1/access/ip-rules", s.admin(s.createIPRule))
	mux.HandleFunc("DELETE /api/v1/access/ip-rules/{id}", s.admin(s.deleteIPRule))
	mux.Handle("/", spaHandler())
	return securityHeaders(mux), nil
}

func (s *server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok", "version": buildinfo.Version,
	})
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	ip := s.proxy.clientIP(r)
	now := time.Now()
	rule, hasRule, shouldAuditRule := s.ipRules.match(ip, now)
	if hasRule && rule.Kind == ipRuleDeny {
		if shouldAuditRule {
			s.access.recordAudit(auditEntry{
				ID: newAuditID(), IP: ip, CredentialID: "BLOCKED",
				Success: false, Event: "attack_blocked", Severity: "critical",
				Reason: "疑似攻击：命中 IP 黑名单", RuleID: rule.ID,
				RuleKind: rule.Kind, CreatedAt: now,
			})
		}
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "登录尝试过多，请稍后再试")
		return
	}
	device, knownDevice := s.devices.read(r, now)
	trustedLane := knownDevice || (hasRule && rule.Kind == ipRuleAllow)
	gateKey := "ip:" + ip
	if knownDevice {
		gateKey = "device:" + device.ID
	} else if trustedLane {
		gateKey = "allow:" + ip
	}
	decision, admission := s.logins.begin(gateKey, trustedLane, now)
	if !decision.Allowed {
		if decision.ShouldAudit {
			reason := decision.Reason
			if decision.Severity != "" {
				reason = "疑似攻击：" + reason
			}
			s.access.recordAudit(auditEntry{
				ID: newAuditID(), IP: ip, CredentialID: "RATE-LIMITED",
				Success: false, Event: "attack_limited", Reason: reason,
				Severity: decision.Severity, AttemptCount: decision.AttemptCount,
				KnownDevice: knownDevice, CreatedAt: now,
			})
		}
		w.Header().Set("Retry-After", retryAfterHeader(decision.RetryAfter))
		writeError(w, http.StatusTooManyRequests, "登录尝试过多，请稍后再试")
		return
	}
	var request struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &request); err != nil {
		admission.cancel()
		s.access.recordAudit(auditEntry{
			ID: newAuditID(), IP: ip, CredentialID: "INVALID-REQUEST",
			Success: false, Event: "login", Reason: "请求格式不正确",
			KnownDevice: knownDevice, CreatedAt: now,
		})
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	identity, authenticated := s.access.authenticate(request.Password)
	request.Password = ""
	assessment := admission.finish(authenticated, time.Now())
	if !authenticated {
		s.access.recordAudit(auditEntry{
			ID: newAuditID(), IP: ip, CredentialID: "UNMATCHED",
			Success: false, Event: "login", Reason: assessment.Reason,
			Severity: assessment.Severity, AttemptCount: assessment.Failures,
			KnownDevice: knownDevice, CreatedAt: now,
		})
		writeError(w, http.StatusUnauthorized, "密码不正确")
		return
	}
	token, err := s.sessions.create(identity)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法创建会话")
		return
	}
	s.access.recordAudit(auditEntry{
		ID: newAuditID(), IP: ip, CredentialID: identity.CredentialID,
		Role: identity.Role, Success: true, Event: "login",
		KnownDevice: knownDevice, CreatedAt: now,
	})
	secure := s.secureCookie(r)
	http.SetCookie(w, &http.Cookie{
		Name: "hearth_session", Value: token, Path: "/", HttpOnly: true,
		Secure: secure, SameSite: http.SameSiteStrictMode,
	})
	var deviceCookie *http.Cookie
	if !knownDevice {
		deviceCookie, _, err = s.devices.newCookie(now, secure)
	} else if now.Sub(device.IssuedAt) >= deviceCookieRefreshAge {
		deviceCookie, err = s.devices.refreshCookie(device, now, secure)
	}
	if err != nil {
		slog.Warn("issue known-device cookie", "error", err)
	} else if deviceCookie != nil {
		http.SetCookie(w, deviceCookie)
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
		overview.Activities = permittedActivities(identity, overview.Activities)
	}
	writeJSON(w, http.StatusOK, overview)
}

func permittedActivities(identity principal, activities []panel.Activity) []panel.Activity {
	visible := make([]panel.Activity, 0, len(activities))
	for _, activity := range activities {
		permission, ok := permissionForAction(activity.Action)
		if activity.Status == "running" && ok && hasPermission(identity, permission) {
			visible = append(visible, activity)
		}
	}
	return visible
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
	activity, err := s.service.RunAction(r.PathValue("id"), request)
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
	case "update", "check-update":
		return permissionGameUpdate, true
	case "backup":
		return permissionGameBackup, true
	default:
		return "", false
	}
}

func (s *server) palworldSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.service.PalworldSettings()
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if identity, ok := principalFromContext(r.Context()); ok && identity.Role == roleMember {
		settings = memberPalworldSettings(settings)
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *server) updatePalworldSettings(w http.ResponseWriter, r *http.Request) {
	var patch panel.PalworldSettingsPatch
	if err := decodeJSON(r, &patch); err != nil {
		writeError(w, http.StatusBadRequest, "配置增量格式不正确")
		return
	}
	before, err := s.service.PalworldSettings()
	if err != nil {
		writeServiceError(w, err)
		return
	}
	identity, _ := principalFromContext(r.Context())
	if identity.Role == roleMember {
		if key, ok := firstDisallowedMemberSetting(before, patch); ok {
			writeError(w, http.StatusForbidden, "成员不能修改系统或高风险参数："+key)
			return
		}
		if err := validateMemberSettingValues(patch); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	updated, err := s.service.UpdatePalworldSettings(patch)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if entry, ok := buildConfigAuditEntry(before, updated, patch, identity, s.proxy.clientIP(r)); ok {
		recordConfigAudit(s.configAudits, entry)
	}
	if identity.Role == roleMember {
		updated = memberPalworldSettings(updated)
	}
	writeJSON(w, http.StatusOK, updated)
}

func memberPalworldSettings(document panel.PalworldSettings) panel.PalworldSettings {
	document.Raw = ""
	groups := make([]panel.SettingGroup, 0, len(document.Groups))
	for _, group := range document.Groups {
		settings := make([]panel.Setting, 0, len(group.Settings))
		for _, setting := range group.Settings {
			if setting.MemberEditable && panel.IsMemberEditablePalworldSetting(setting.Key) {
				settings = append(settings, setting)
			}
		}
		if len(settings) == 0 {
			continue
		}
		group.Settings = settings
		groups = append(groups, group)
	}
	document.Groups = groups
	return document
}

func firstDisallowedMemberSetting(
	document panel.PalworldSettings,
	patch panel.PalworldSettingsPatch,
) (string, bool) {
	available := settingsByKey(document)
	keys := make([]string, 0, len(patch.Changes))
	for key := range patch.Changes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		setting, ok := available[key]
		if !ok || !setting.MemberEditable || !panel.IsMemberEditablePalworldSetting(key) {
			return key, true
		}
	}
	return "", false
}

func validateMemberSettingValues(patch panel.PalworldSettingsPatch) error {
	for key, value := range patch.Changes {
		text, ok := value.(string)
		if !ok {
			continue
		}
		limit := 0
		switch key {
		case "ServerName":
			limit = 128
		case "ServerDescription":
			limit = 1024
		case "DenyTechnologyList":
			limit = 16 << 10
		}
		if limit > 0 && len([]rune(text)) > limit {
			return fmt.Errorf("成员提交的参数 %s 超过长度限制", key)
		}
	}
	return nil
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
	return s.proxy.forwardedProto(r) == "https"
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
	mediaType := strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0])
	if !strings.EqualFold(mediaType, "application/json") {
		return errors.New("content type must be application/json")
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > limit {
		return errors.New("request body is too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain exactly one JSON value")
	}
	return nil
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

const maxSessions = 1000

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
	if len(s.sessions) >= maxSessions {
		var oldestToken string
		var oldestExpiry time.Time
		for key, item := range s.sessions {
			if oldestToken == "" || item.ExpiresAt.Before(oldestExpiry) {
				oldestToken, oldestExpiry = key, item.ExpiresAt
			}
		}
		delete(s.sessions, oldestToken)
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
