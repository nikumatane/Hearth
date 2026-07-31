package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	if len(cookies) != 1 || cookies[0].Name != "hearth_session" {
		t.Fatalf("login cookies = %#v", cookies)
	}

	overviewRequest := httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)
	overviewRequest.AddCookie(cookies[0])
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
	gate := newLoginGate(2, time.Minute)
	now := time.Now()
	gate.recordFailure(now)
	gate.recordFailure(now)

	if allowed, retryAfter := gate.allow(now); allowed || retryAfter <= 0 {
		t.Fatalf("allow() = %v, %v; want blocked with retry duration", allowed, retryAfter)
	}

	gate.reset()
	if allowed, _ := gate.allow(now); !allowed {
		t.Fatal("allow() remained blocked after reset")
	}
}

func TestLoginUsesSecureCookieBehindHTTPSProxy(t *testing.T) {
	handler := newTestHandler(t, config.Config{AdminPassword: "correct"})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session", bytes.NewBufferString(`{"password":"correct"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Forwarded-Proto", "https")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure {
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
	if len(cookies) != 1 {
		t.Fatalf("login cookies = %#v", cookies)
	}
	return cookies[0]
}

func newTestHandler(t *testing.T, cfg config.Config) http.Handler {
	t.Helper()
	handler, err := New(cfg, panel.NewDemoService())
	if err != nil {
		t.Fatalf("create HTTP handler: %v", err)
	}
	return handler
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
