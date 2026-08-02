package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hearth/internal/config"
	"hearth/internal/panel"
)

func TestLoginAndAuthenticatedOverview(t *testing.T) {
	handler := newTestHandler(t, config.Config{AdminPassword: "correct"})

	session := httptest.NewRecorder()
	handler.ServeHTTP(session, httptest.NewRequest(http.MethodGet, "/api/v1/session", nil))
	var anonymous struct {
		Authenticated bool   `json:"authenticated"`
		Version       string `json:"version"`
	}
	if err := json.Unmarshal(session.Body.Bytes(), &anonymous); err != nil {
		t.Fatalf("decode anonymous session: %v", err)
	}
	if session.Code != http.StatusOK || anonymous.Authenticated || anonymous.Version == "" {
		t.Fatalf("anonymous session status = %d body = %s", session.Code, session.Body.String())
	}

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	body, _ := json.Marshal(map[string]string{"password": "correct"})
	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/session", bytes.NewReader(body))
	loginRequest.Header.Set("Content-Type", "application/json")
	login := httptest.NewRecorder()
	handler.ServeHTTP(login, loginRequest)
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d body = %s", login.Code, login.Body.String())
	}
	cookies := login.Result().Cookies()
	sessionCookie := cookieNamed(cookies, "hearth_session")
	if sessionCookie == nil || cookieNamed(cookies, deviceCookieName) == nil {
		t.Fatalf("login cookies = %#v", cookies)
	}

	overviewRequest := httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)
	overviewRequest.AddCookie(sessionCookie)
	overview := httptest.NewRecorder()
	handler.ServeHTTP(overview, overviewRequest)
	if overview.Code != http.StatusOK {
		t.Fatalf("overview status = %d body = %s", overview.Code, overview.Body.String())
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	handler := newTestHandler(t, config.Config{AdminPassword: "correct"})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session", bytes.NewBufferString(`{"password":"wrong"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestLoginGateLimitsFailuresAndResets(t *testing.T) {
	gate := newLoginGate()
	now := time.Now()
	for attempt := 1; attempt <= 5; attempt++ {
		decision, admission := gate.begin("ip:198.51.100.10", false, now)
		if !decision.Allowed || admission == nil {
			t.Fatalf("attempt %d was unexpectedly blocked: %#v", attempt, decision)
		}
		assessment := admission.finish(false, now)
		if assessment.Failures != attempt {
			t.Fatalf("attempt failures = %d; want %d", assessment.Failures, attempt)
		}
	}
	decision, _ := gate.begin("ip:198.51.100.10", false, now)
	if decision.Allowed || decision.RetryAfter <= 0 || decision.Severity != "warning" {
		t.Fatalf("begin() = %#v; want warning backoff", decision)
	}
	if other, admission := gate.begin("ip:198.51.100.11", false, now); !other.Allowed {
		t.Fatal("one source IP blocked a different source IP")
	} else {
		admission.cancel()
	}

	decision, admission := gate.begin("ip:198.51.100.10", false, now.Add(time.Second))
	if !decision.Allowed {
		t.Fatalf("begin() remained blocked after backoff: %#v", decision)
	}
	admission.finish(true, now.Add(time.Second))
	if reset, admission := gate.begin("ip:198.51.100.10", false, now.Add(time.Second)); !reset.Allowed {
		t.Fatal("begin() remained blocked after successful verification")
	} else {
		admission.cancel()
	}
}

func TestSessionStoreHasBoundedSize(t *testing.T) {
	store := newSessionStore()
	for index := 0; index < maxSessions+25; index++ {
		if _, err := store.create(principal{
			Role: roleMember, CredentialID: "M-BOUNDED",
		}); err != nil {
			t.Fatalf("create session %d: %v", index, err)
		}
	}
	if len(store.sessions) != maxSessions {
		t.Fatalf("session count = %d; want %d", len(store.sessions), maxSessions)
	}
}

func TestDecodeJSONRejectsTrailingAndOversizedBodies(t *testing.T) {
	for _, test := range []struct {
		name  string
		body  string
		limit int64
	}{
		{name: "trailing value", body: `{"value":"ok"} {"value":"extra"}`, limit: 1024},
		{name: "trailing junk", body: `{"value":"ok"} junk`, limit: 1024},
		{name: "oversized", body: `{"value":"` + strings.Repeat("x", 64) + `"}`, limit: 32},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			var target struct {
				Value string `json:"value"`
			}
			if err := decodeJSONLimit(request, &target, test.limit); err == nil {
				t.Fatal("decodeJSONLimit() accepted an invalid request body")
			}
		})
	}
}

func TestLoginUsesSecureCookieBehindHTTPSProxy(t *testing.T) {
	handler := newTestHandler(t, config.Config{AdminPassword: "correct"})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session", bytes.NewBufferString(`{"password":"correct"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Forwarded-Proto", "https")
	request.RemoteAddr = "127.0.0.1:12345"
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)
	cookies := response.Result().Cookies()
	if cookieNamed(cookies, "hearth_session") == nil ||
		!cookieNamed(cookies, "hearth_session").Secure ||
		cookieNamed(cookies, deviceCookieName) == nil ||
		!cookieNamed(cookies, deviceCookieName).Secure {
		t.Fatalf("cookies = %#v; want a Secure session cookie", cookies)
	}
}

func TestGameActionUnsafeConfirmationBoundary(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "restart accepts explicit fallback confirmation",
			body:       `{"action":"restart","allowUnsafe":true}`,
			wantStatus: http.StatusAccepted,
		},
		{
			name:       "update cannot use force fallback",
			body:       `{"action":"update","allowUnsafe":true}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "version check is an accepted read-only Steam task",
			body:       `{"action":"check-update"}`,
			wantStatus: http.StatusAccepted,
		},
		{
			name:       "version check cannot use force fallback",
			body:       `{"action":"check-update","allowUnsafe":true}`,
			wantStatus: http.StatusBadRequest,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := newTestHandler(t, config.Config{AdminPassword: "correct"})
			cookie := loginTestHandler(t, handler)
			request := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/games/palworld/actions",
				bytes.NewBufferString(test.body),
			)
			request.Header.Set("Content-Type", "application/json")
			request.AddCookie(cookie)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
		})
	}
}

func loginTestHandler(t *testing.T, handler http.Handler) *http.Cookie {
	t.Helper()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/session",
		bytes.NewBufferString(`{"password":"correct"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("login status = %d body = %s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	sessionCookie := cookieNamed(cookies, "hearth_session")
	if sessionCookie == nil {
		t.Fatalf("login cookies = %#v", cookies)
	}
	return sessionCookie
}

func cookieNamed(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func newTestHandler(t *testing.T, cfg config.Config) http.Handler {
	t.Helper()
	handler, err := New(cfg, panel.NewDemoService())
	if err != nil {
		t.Fatalf("create HTTP handler: %v", err)
	}
	return handler
}

type logTestService struct {
	*panel.DemoService
	overview panel.Overview
}

func (s *logTestService) Overview() panel.Overview {
	return s.overview
}

func TestLogsAreLinkedAndLoadedOnDemand(t *testing.T) {
	directory := t.TempDir()
	panelLog := filepath.Join(directory, "hearth.log")
	if err := os.WriteFile(panelLog, []byte("panel-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	logDirectory := filepath.Join(directory, "panel-logs")
	if err := os.MkdirAll(logDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	const taskLogID = "steamcmd-update-20260802-120000.000000001.log"
	if err := os.WriteFile(filepath.Join(logDirectory, taskLogID), []byte("task-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logDirectory, "unreferenced.log"), []byte("hidden\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	service := &logTestService{
		DemoService: panel.NewDemoService(),
		overview: panel.Overview{Activities: []panel.Activity{{
			ID: "activity-1", GameID: "palworld", Action: "update",
			Title: "服务器已是最新版", Status: "success", CreatedAt: now, UpdatedAt: now,
			Logs: []panel.LogRef{{ID: taskLogID, Label: "SteamCMD 更新日志"}},
		}}},
	}
	handler, err := New(config.Config{
		AdminPassword: "correct", LogFile: panelLog,
		Games: config.GamesConfig{Palworld: config.GameConfig{InstallDir: directory}},
	}, service)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	cookie := loginTestHandler(t, handler)

	metadataRequest := httptest.NewRequest(http.MethodGet, "/api/v1/logs", nil)
	metadataRequest.AddCookie(cookie)
	metadata := httptest.NewRecorder()
	handler.ServeHTTP(metadata, metadataRequest)
	if metadata.Code != http.StatusOK {
		t.Fatalf("metadata status = %d body = %s", metadata.Code, metadata.Body.String())
	}
	if strings.Contains(metadata.Body.String(), "panel-secret") || strings.Contains(metadata.Body.String(), "task-secret") {
		t.Fatalf("metadata eagerly included log content: %s", metadata.Body.String())
	}
	if !strings.Contains(metadata.Body.String(), taskLogID) {
		t.Fatalf("metadata omitted activity log reference: %s", metadata.Body.String())
	}

	logRequest := httptest.NewRequest(http.MethodGet, "/api/v1/logs/"+taskLogID, nil)
	logRequest.AddCookie(cookie)
	logResponse := httptest.NewRecorder()
	handler.ServeHTTP(logResponse, logRequest)
	if logResponse.Code != http.StatusOK || !strings.Contains(logResponse.Body.String(), "task-secret") {
		t.Fatalf("task log status = %d body = %s", logResponse.Code, logResponse.Body.String())
	}

	unreferencedRequest := httptest.NewRequest(http.MethodGet, "/api/v1/logs/unreferenced.log", nil)
	unreferencedRequest.AddCookie(cookie)
	unreferencedResponse := httptest.NewRecorder()
	handler.ServeHTTP(unreferencedResponse, unreferencedRequest)
	if unreferencedResponse.Code != http.StatusNotFound {
		t.Fatalf("unreferenced log status = %d body = %s", unreferencedResponse.Code, unreferencedResponse.Body.String())
	}
}

func TestSecurityHeadersAllowOnlyWasmEvaluation(t *testing.T) {
	handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	policy := response.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "script-src 'self' 'wasm-unsafe-eval'") {
		t.Fatalf("Content-Security-Policy does not permit WebAssembly: %q", policy)
	}
	if strings.Contains(policy, "'unsafe-eval'") {
		t.Fatalf("Content-Security-Policy permits general eval: %q", policy)
	}
}

func TestSecurityHeadersCachePolicy(t *testing.T) {
	handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	tests := []struct {
		path string
		want string
	}{
		{path: "/", want: "no-store, max-age=0"},
		{path: "/favicon.svg", want: "no-store, max-age=0"},
		{path: "/api/v1/health", want: "no-store"},
		{path: "/assets/app-abc123.js", want: "public, max-age=31536000, immutable"},
	}
	for _, test := range tests {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if got := response.Header().Get("Cache-Control"); got != test.want {
			t.Errorf("%s Cache-Control = %q; want %q", test.path, got, test.want)
		}
	}
}

func TestVersionCheckUsesUpdatePermission(t *testing.T) {
	permission, ok := permissionForAction("check-update")
	if !ok {
		t.Fatal("check-update action was rejected")
	}
	if permission != permissionGameUpdate {
		t.Fatalf("permission = %q; want %q", permission, permissionGameUpdate)
	}
}

func TestMemberOverviewOnlyIncludesPermittedRunningActivities(t *testing.T) {
	identity := principal{Role: roleMember, Permissions: []string{permissionGameControl}}
	activities := []panel.Activity{
		{ID: "running-control", Action: "restart", Status: "running"},
		{ID: "completed-control", Action: "restart", Status: "success"},
		{ID: "failed-control", Action: "stop", Status: "error"},
		{ID: "running-update", Action: "update", Status: "running"},
	}

	visible := permittedActivities(identity, activities)
	if len(visible) != 1 || visible[0].ID != "running-control" {
		t.Fatalf("visible activities = %#v; want only permitted running activity", visible)
	}
}
