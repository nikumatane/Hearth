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

	"hearth/internal/config"
)

func TestAccessStorePersistsOnlyPasswordDigests(t *testing.T) {
	directory := t.TempDir()
	memberPath := filepath.Join(directory, "member-credentials.json")
	auditPath := filepath.Join(directory, "login-audit.jsonl")

	store, err := newAccessStore("admin-secret-123", memberPath, auditPath)
	if err != nil {
		t.Fatalf("create access store: %v", err)
	}
	member, err := store.createMember("friend-secret-123")
	if err != nil {
		t.Fatalf("create member: %v", err)
	}

	data, err := os.ReadFile(memberPath)
	if err != nil {
		t.Fatalf("read member file: %v", err)
	}
	if strings.Contains(string(data), "admin-secret-123") ||
		strings.Contains(string(data), "friend-secret-123") {
		t.Fatalf("member file contains a plaintext password: %s", data)
	}

	identity, ok := store.authenticate("friend-secret-123")
	if !ok || identity.Role != roleMember || identity.CredentialID != member.ID {
		t.Fatalf("member authentication = %#v, %v", identity, ok)
	}

	reloaded, err := newAccessStore("admin-secret-123", memberPath, auditPath)
	if err != nil {
		t.Fatalf("reload access store: %v", err)
	}
	if _, ok := reloaded.authenticate("friend-secret-123"); !ok {
		t.Fatal("persisted member password did not authenticate")
	}
	if _, err := reloaded.updateMember(member.ID, "friend-secret-456"); err != nil {
		t.Fatalf("update member: %v", err)
	}
	if _, ok := reloaded.authenticate("friend-secret-123"); ok {
		t.Fatal("old member password remained valid after update")
	}
	if _, ok := reloaded.authenticate("friend-secret-456"); !ok {
		t.Fatal("new member password did not authenticate")
	}
	if err := reloaded.deleteMember(member.ID); err != nil {
		t.Fatalf("delete member: %v", err)
	}
	if _, ok := reloaded.authenticate("friend-secret-456"); ok {
		t.Fatal("deleted member password remained valid")
	}
}

func TestAdminAndMemberPermissionsAndLoginAudit(t *testing.T) {
	directory := t.TempDir()
	cfg := config.Config{
		AdminPassword: "admin-secret-123",
		AccessFile:    filepath.Join(directory, "member-credentials.json"),
		AuditFile:     filepath.Join(directory, "login-audit.jsonl"),
	}
	handler := newTestHandler(t, cfg)

	adminCookie, adminRole := loginForTest(t, handler, "admin-secret-123", "203.0.113.11")
	if adminRole != roleAdmin {
		t.Fatalf("admin login role = %q", adminRole)
	}

	create := requestForTest(
		t, handler, http.MethodPost, "/api/v1/access/members",
		`{"password":"friend-secret-123"}`, adminCookie,
	)
	if create.Code != http.StatusCreated {
		t.Fatalf("create member status = %d body = %s", create.Code, create.Body.String())
	}
	var member memberView
	if err := json.Unmarshal(create.Body.Bytes(), &member); err != nil {
		t.Fatalf("decode member: %v", err)
	}

	memberCookie, memberRole := loginForTest(t, handler, "friend-secret-123", "198.51.100.22")
	if memberRole != roleMember {
		t.Fatalf("member login role = %q", memberRole)
	}
	failedLogin := httptest.NewRequest(
		http.MethodPost, "/api/v1/session",
		bytes.NewBufferString(`{"password":"definitely-wrong-password"}`),
	)
	failedLogin.Header.Set("Content-Type", "application/json")
	failedLogin.Header.Set("X-Forwarded-For", "192.0.2.33")
	failedResponse := httptest.NewRecorder()
	handler.ServeHTTP(failedResponse, failedLogin)
	if failedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("failed login status = %d body = %s", failedResponse.Code, failedResponse.Body.String())
	}

	for _, test := range []struct {
		path string
		want int
	}{
		{path: "/api/v1/overview", want: http.StatusOK},
		{path: "/api/v1/logs", want: http.StatusForbidden},
		{path: "/api/v1/games/palworld/world-option", want: http.StatusForbidden},
		{path: "/api/v1/access/members", want: http.StatusForbidden},
		{path: "/api/v1/access/audit", want: http.StatusForbidden},
	} {
		response := requestForTest(t, handler, http.MethodGet, test.path, "", memberCookie)
		if response.Code != test.want {
			t.Errorf("%s status = %d body = %s; want %d", test.path, response.Code, response.Body.String(), test.want)
		}
	}

	audit := requestForTest(t, handler, http.MethodGet, "/api/v1/access/audit", "", adminCookie)
	if audit.Code != http.StatusOK {
		t.Fatalf("audit status = %d body = %s", audit.Code, audit.Body.String())
	}
	if !strings.Contains(audit.Body.String(), "203.0.113.11") ||
		!strings.Contains(audit.Body.String(), "198.51.100.22") ||
		!strings.Contains(audit.Body.String(), "192.0.2.33") ||
		!strings.Contains(audit.Body.String(), "UNMATCHED") ||
		!strings.Contains(audit.Body.String(), member.ID) {
		t.Fatalf("audit response is missing login identities: %s", audit.Body.String())
	}
	auditData, err := os.ReadFile(cfg.AuditFile)
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	if strings.Contains(string(auditData), "admin-secret-123") ||
		strings.Contains(string(auditData), "friend-secret-123") ||
		strings.Contains(string(auditData), "definitely-wrong-password") {
		t.Fatalf("audit file contains a plaintext password: %s", auditData)
	}

	deleted := requestForTest(
		t, handler, http.MethodDelete, "/api/v1/access/members/"+member.ID, "", adminCookie,
	)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete member status = %d body = %s", deleted.Code, deleted.Body.String())
	}
	revoked := requestForTest(t, handler, http.MethodGet, "/api/v1/overview", "", memberCookie)
	if revoked.Code != http.StatusUnauthorized {
		t.Fatalf("deleted member session status = %d; want unauthorized", revoked.Code)
	}
}

func loginForTest(t *testing.T, handler http.Handler, password, ip string) (*http.Cookie, string) {
	t.Helper()
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/session",
		bytes.NewBufferString(`{"password":`+mustJSON(t, password)+`}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Forwarded-For", ip)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("login status = %d body = %s", response.Code, response.Body.String())
	}
	var session struct {
		Role string `json:"role"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("login cookies = %#v", cookies)
	}
	return cookies[0], session.Role
}

func requestForTest(
	t *testing.T,
	handler http.Handler,
	method, path, body string,
	cookie *http.Cookie,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func mustJSON(t *testing.T, value string) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return string(data)
}
