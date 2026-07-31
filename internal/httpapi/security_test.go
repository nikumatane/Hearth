package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hearth/internal/config"
)

func TestProxyResolverTrustBoundary(t *testing.T) {
	resolver, err := newProxyResolver([]string{"127.0.0.0/8", "10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	untrusted := httptest.NewRequest(http.MethodGet, "/", nil)
	untrusted.RemoteAddr = "198.51.100.7:4321"
	untrusted.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := resolver.clientIP(untrusted); got != "198.51.100.7" {
		t.Fatalf("untrusted forwarded IP = %q", got)
	}

	trusted := httptest.NewRequest(http.MethodGet, "/", nil)
	trusted.RemoteAddr = "127.0.0.1:4321"
	trusted.Header.Set("X-Forwarded-For", "192.0.2.66, 203.0.113.20, 10.2.3.4")
	if got := resolver.clientIP(trusted); got != "203.0.113.20" {
		t.Fatalf("trusted forwarded IP = %q; want rightmost untrusted hop", got)
	}
	trusted.Header.Set("X-Forwarded-For", "malformed, 10.2.3.4")
	if got := resolver.clientIP(trusted); got != "127.0.0.1" {
		t.Fatalf("malformed header resolved to %q; want fail-closed peer", got)
	}
}

func TestProxyResolverOnlyTrustsForwardedProtoFromTrustedPeer(t *testing.T) {
	resolver, err := newProxyResolver(nil)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "198.51.100.7:4321"
	request.Header.Set("X-Forwarded-Proto", "https")
	if got := resolver.forwardedProto(request); got != "" {
		t.Fatalf("untrusted forwarded proto = %q", got)
	}
	request.RemoteAddr = "127.0.0.1:4321"
	if got := resolver.forwardedProto(request); got != "https" {
		t.Fatalf("trusted forwarded proto = %q", got)
	}
	request.Header.Set("X-Forwarded-Proto", "https, malformed")
	if got := resolver.forwardedProto(request); got != "" {
		t.Fatalf("malformed forwarded proto = %q", got)
	}
}

func TestKnownDeviceCookieIsSignedAndExpires(t *testing.T) {
	manager, err := newDeviceCookieManager(filepath.Join(t.TempDir(), "device.key"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Second)
	cookie, device, err := manager.newCookie(now, true)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	request.AddCookie(cookie)
	got, ok := manager.read(request, now.Add(time.Hour))
	if !ok || got.ID != device.ID {
		t.Fatalf("read known device = %#v, %v", got, ok)
	}

	tampered := *cookie
	tampered.Value += "x"
	request = httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	request.AddCookie(&tampered)
	if _, ok := manager.read(request, now.Add(time.Hour)); ok {
		t.Fatal("tampered device cookie was accepted")
	}
	if _, ok := manager.read(httptest.NewRequest(http.MethodGet, "/", nil), now); ok {
		t.Fatal("missing device cookie was accepted")
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	request.AddCookie(cookie)
	if _, ok := manager.read(request, now.Add(deviceCookieTTL+time.Second)); ok {
		t.Fatal("expired device cookie was accepted")
	}
}

func TestKnownDeviceCookieNeverAuthenticatesWithoutPassword(t *testing.T) {
	handler := newTestHandler(t, config.Config{AdminPassword: "correct"})
	login := httptest.NewRequest(
		http.MethodPost, "/api/v1/session", strings.NewReader(`{"password":"correct"}`),
	)
	login.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, login)
	deviceCookie := cookieNamed(loginResponse.Result().Cookies(), deviceCookieName)
	if deviceCookie == nil {
		t.Fatal("successful login did not issue a known-device cookie")
	}

	overview := httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)
	overview.AddCookie(deviceCookie)
	overviewResponse := httptest.NewRecorder()
	handler.ServeHTTP(overviewResponse, overview)
	if overviewResponse.Code != http.StatusUnauthorized {
		t.Fatalf("device-only overview status = %d", overviewResponse.Code)
	}

	wrongLogin := httptest.NewRequest(
		http.MethodPost, "/api/v1/session", strings.NewReader(`{"password":"wrong"}`),
	)
	wrongLogin.Header.Set("Content-Type", "application/json")
	wrongLogin.AddCookie(deviceCookie)
	wrongResponse := httptest.NewRecorder()
	handler.ServeHTTP(wrongResponse, wrongLogin)
	if wrongResponse.Code != http.StatusUnauthorized {
		t.Fatalf("device-only wrong-password login status = %d", wrongResponse.Code)
	}
}

func TestIPRulesBlockBeforePasswordAndAllowDoesNotBypassPassword(t *testing.T) {
	handler := newTestHandler(t, config.Config{AdminPassword: "correct"})
	adminCookie := loginTestHandler(t, handler)

	deny := requestForTest(
		t, handler, http.MethodPost, "/api/v1/access/ip-rules",
		`{"ip":"198.51.100.44","kind":"deny"}`, adminCookie,
	)
	if deny.Code != http.StatusCreated {
		t.Fatalf("create deny rule status = %d body = %s", deny.Code, deny.Body.String())
	}
	blockedRequest := httptest.NewRequest(
		http.MethodPost, "/api/v1/session", bytes.NewBufferString(`{"password":"correct"}`),
	)
	blockedRequest.Header.Set("Content-Type", "application/json")
	blockedRequest.RemoteAddr = "198.51.100.44:9999"
	blocked := httptest.NewRecorder()
	handler.ServeHTTP(blocked, blockedRequest)
	if blocked.Code != http.StatusTooManyRequests {
		t.Fatalf("blacklisted login status = %d body = %s", blocked.Code, blocked.Body.String())
	}

	allow := requestForTest(
		t, handler, http.MethodPost, "/api/v1/access/ip-rules",
		`{"ip":"198.51.100.44","kind":"allow"}`, adminCookie,
	)
	if allow.Code != http.StatusCreated {
		t.Fatalf("replace with allow rule status = %d body = %s", allow.Code, allow.Body.String())
	}
	wrongRequest := httptest.NewRequest(
		http.MethodPost, "/api/v1/session", bytes.NewBufferString(`{"password":"wrong"}`),
	)
	wrongRequest.Header.Set("Content-Type", "application/json")
	wrongRequest.RemoteAddr = "198.51.100.44:9999"
	wrong := httptest.NewRecorder()
	handler.ServeHTTP(wrong, wrongRequest)
	if wrong.Code != http.StatusUnauthorized {
		t.Fatalf("allowlisted wrong-password status = %d body = %s", wrong.Code, wrong.Body.String())
	}
}

func TestAttackThresholdIsExplicitInLoginAudit(t *testing.T) {
	handler := newTestHandler(t, config.Config{AdminPassword: "correct"})
	for attempt := 0; attempt < 5; attempt++ {
		request := httptest.NewRequest(
			http.MethodPost, "/api/v1/session", strings.NewReader(`{"password":"wrong"}`),
		)
		request.Header.Set("Content-Type", "application/json")
		request.RemoteAddr = "198.51.100.91:9999"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d", attempt+1, response.Code)
		}
	}
	adminCookie := loginTestHandler(t, handler)
	auditResponse := requestForTest(
		t, handler, http.MethodGet, "/api/v1/access/audit", "", adminCookie,
	)
	var body struct {
		Entries []auditEntry `json:"entries"`
	}
	if err := json.Unmarshal(auditResponse.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, entry := range body.Entries {
		if entry.IP == "198.51.100.91" && entry.Severity == "warning" &&
			entry.AttemptCount == 5 && strings.Contains(entry.Reason, "疑似") {
			return
		}
	}
	t.Fatalf("audit entries do not contain an explicit attack threshold: %#v", body.Entries)
}

func TestIPRulesRejectLoopbackAndTrustedProxyAddresses(t *testing.T) {
	handler := newTestHandler(t, config.Config{AdminPassword: "correct"})
	adminCookie := loginTestHandler(t, handler)
	for _, ip := range []string{"127.0.0.1", "::1"} {
		response := requestForTest(
			t, handler, http.MethodPost, "/api/v1/access/ip-rules",
			`{"ip":`+mustJSON(t, ip)+`,"kind":"deny","confirmCurrentIp":true}`,
			adminCookie,
		)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("trusted address %s status = %d body = %s", ip, response.Code, response.Body.String())
		}
	}
}

func TestIPRuleStorePersistsAndExpiresRules(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ip-rules.json")
	store, err := newIPRuleStore(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	expiry := now.Add(time.Hour)
	rule, err := store.add("2001:db8::10", ipRuleDeny, "test", "ADMIN", &expiry, now)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := newIPRuleStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if rules := reloaded.all(now); len(rules) != 1 || rules[0].ID != rule.ID {
		t.Fatalf("reloaded rules = %#v", rules)
	}
	if rules := reloaded.all(expiry.Add(time.Second)); len(rules) != 0 {
		t.Fatalf("expired rules = %#v", rules)
	}
}

func TestLoginGateBoundsConcurrentPasswordVerification(t *testing.T) {
	gate := newLoginGate()
	now := time.Now()
	first, firstAdmission := gate.begin("ip:198.51.100.1", false, now)
	second, secondAdmission := gate.begin("ip:198.51.100.2", false, now)
	third, _ := gate.begin("ip:198.51.100.3", false, now)
	if !first.Allowed || !second.Allowed || third.Allowed ||
		third.Reason != "登录校验繁忙" {
		t.Fatalf("slot decisions = %#v %#v %#v", first, second, third)
	}
	trusted, trustedAdmission := gate.begin("device:known", true, now)
	if !trusted.Allowed {
		t.Fatalf("trusted slot was starved by unknown attempts: %#v", trusted)
	}
	firstAdmission.cancel()
	secondAdmission.cancel()
	trustedAdmission.cancel()
}
