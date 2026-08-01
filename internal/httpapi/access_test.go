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

func TestAccessStorePersistsOnlyPasswordDigests(t *testing.T) {
	directory := t.TempDir()
	memberPath := filepath.Join(directory, "member-credentials.json")
	auditPath := filepath.Join(directory, "login-audit.jsonl")

	store, err := newAccessStore("admin-secret-123", memberPath, auditPath)
	if err != nil {
		t.Fatalf("create access store: %v", err)
	}
	member, err := store.createMember(
		"friend-secret-123",
		[]string{permissionGameControl, legacyPermissionPalworldSettings},
	)
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
	if strings.Contains(string(data), legacyPermissionPalworldSettings+`"`) ||
		!strings.Contains(string(data), permissionPalworldGameplay) {
		t.Fatalf("legacy settings permission was not migrated safely: %s", data)
	}

	identity, ok := store.authenticate("friend-secret-123")
	if !ok || identity.Role != roleMember || identity.CredentialID != member.ID ||
		!hasPermission(identity, permissionGameControl) ||
		!hasPermission(identity, permissionPalworldGameplay) {
		t.Fatalf("member authentication = %#v, %v", identity, ok)
	}

	legacyData := strings.ReplaceAll(
		string(data), permissionPalworldGameplay, legacyPermissionPalworldSettings,
	)
	if err := os.WriteFile(memberPath, []byte(legacyData), 0o600); err != nil {
		t.Fatalf("seed legacy member permission: %v", err)
	}
	reloaded, err := newAccessStore("admin-secret-123", memberPath, auditPath)
	if err != nil {
		t.Fatalf("reload access store: %v", err)
	}
	migratedData, err := os.ReadFile(memberPath)
	if err != nil {
		t.Fatalf("read migrated member file: %v", err)
	}
	if strings.Contains(string(migratedData), legacyPermissionPalworldSettings+`"`) ||
		!strings.Contains(string(migratedData), permissionPalworldGameplay) {
		t.Fatalf("stored legacy permission was not persisted as gameplay permission: %s", migratedData)
	}
	if _, ok := reloaded.authenticate("friend-secret-123"); !ok {
		t.Fatal("persisted member password did not authenticate")
	}
	newPassword := "friend-secret-456"
	newPermissions := []string{permissionGameBackup}
	if _, _, err := reloaded.updateMember(member.ID, &newPassword, &newPermissions); err != nil {
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

func TestValidateMemberPasswordUsesTenCharacterMinimum(t *testing.T) {
	if err := validateMemberPassword("1234567890"); err != nil {
		t.Fatalf("10-character password was rejected: %v", err)
	}
	if err := validateMemberPassword("123456789"); err == nil {
		t.Fatal("9-character password was accepted")
	}
	if err := validateMemberPassword("中文密码十位字符测试"); err != nil {
		t.Fatalf("10-character Unicode password was rejected: %v", err)
	}
}

func TestLoginAuditRotatesAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "login-audit.jsonl")
	filler := bytes.Repeat([]byte("{}\n"), maxLoginAuditSize/3+1)
	if err := os.WriteFile(path, filler, 0o600); err != nil {
		t.Fatalf("seed login audit: %v", err)
	}
	store := &accessStore{auditPath: path, audits: []auditEntry{}}
	entry := auditEntry{
		ID: "login-1", IP: "198.51.100.20", CredentialID: "ADMIN",
		Role: roleAdmin, Success: true, CreatedAt: time.Now(),
	}
	store.recordAudit(entry)
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("rotated login audit not found: %v", err)
	}
	reloaded := &accessStore{auditPath: path, audits: []auditEntry{}}
	if err := reloaded.loadAudits(); err != nil {
		t.Fatalf("reload login audit: %v", err)
	}
	entries := reloaded.auditEntries()
	if len(entries) != 1 || entries[0].ID != entry.ID {
		t.Fatalf("reloaded login audit = %#v", entries)
	}
}

func TestAdminAndMemberPermissionsAndLoginAudit(t *testing.T) {
	directory := t.TempDir()
	cfg := config.Config{
		AdminPassword:   "admin-secret-123",
		AccessFile:      filepath.Join(directory, "member-credentials.json"),
		AuditFile:       filepath.Join(directory, "login-audit.jsonl"),
		ConfigAuditFile: filepath.Join(directory, "config-audit.jsonl"),
	}
	handler := newTestHandler(t, cfg)

	adminCookie, adminRole := loginForTest(t, handler, "admin-secret-123", "203.0.113.11")
	if adminRole != roleAdmin {
		t.Fatalf("admin login role = %q", adminRole)
	}

	create := requestForTest(
		t, handler, http.MethodPost, "/api/v1/access/members",
		`{"password":"friend-secret-123","permissions":["game.control","palworld.settings"]}`, adminCookie,
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
	failedLogin.RemoteAddr = "127.0.0.1:12345"
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
		{path: "/api/v1/games/palworld/settings", want: http.StatusOK},
		{path: "/api/v1/games/palworld/world-option", want: http.StatusForbidden},
		{path: "/api/v1/access/members", want: http.StatusForbidden},
		{path: "/api/v1/access/audit", want: http.StatusForbidden},
		{path: "/api/v1/access/operation-audit", want: http.StatusForbidden},
		{path: "/api/v1/access/config-audit", want: http.StatusForbidden},
		{path: "/api/v1/access/ip-rules", want: http.StatusForbidden},
	} {
		response := requestForTest(t, handler, http.MethodGet, test.path, "", memberCookie)
		if response.Code != test.want {
			t.Errorf("%s status = %d body = %s; want %d", test.path, response.Code, response.Body.String(), test.want)
		}
	}

	settingsResponse := requestForTest(
		t, handler, http.MethodGet, "/api/v1/games/palworld/settings", "", memberCookie,
	)
	var memberSettings panel.PalworldSettings
	if err := json.Unmarshal(settingsResponse.Body.Bytes(), &memberSettings); err != nil {
		t.Fatalf("decode member settings: %v", err)
	}
	if memberSettings.Raw != "" {
		t.Fatal("member settings response exposed raw INI")
	}
	for _, group := range memberSettings.Groups {
		for _, setting := range group.Settings {
			if !setting.MemberEditable || !panel.IsMemberEditablePalworldSetting(setting.Key) {
				t.Fatalf("member settings exposed disallowed parameter %q", setting.Key)
			}
		}
	}
	deniedSettingsUpdate := requestForTest(
		t, handler, http.MethodPatch, "/api/v1/games/palworld/settings",
		`{"revision":"","changes":{"RESTAPIEnabled":false}}`, memberCookie,
	)
	if deniedSettingsUpdate.Code != http.StatusForbidden {
		t.Fatalf("system setting update status = %d body = %s", deniedSettingsUpdate.Code, deniedSettingsUpdate.Body.String())
	}
	oversizedSettingsUpdate := requestForTest(
		t, handler, http.MethodPatch, "/api/v1/games/palworld/settings",
		`{"revision":"","changes":{"ServerName":"`+strings.Repeat("界", 129)+`"}}`, memberCookie,
	)
	if oversizedSettingsUpdate.Code != http.StatusBadRequest {
		t.Fatalf("oversized setting update status = %d body = %s", oversizedSettingsUpdate.Code, oversizedSettingsUpdate.Body.String())
	}
	allowedSettingsUpdate := requestForTest(
		t, handler, http.MethodPatch, "/api/v1/games/palworld/settings",
		`{"revision":"","changes":{"ServerName":"成员修改后的服务器"}}`, memberCookie,
	)
	if allowedSettingsUpdate.Code != http.StatusOK {
		t.Fatalf("gameplay setting update status = %d body = %s", allowedSettingsUpdate.Code, allowedSettingsUpdate.Body.String())
	}
	configAudit := requestForTest(
		t, handler, http.MethodGet, "/api/v1/access/config-audit", "", adminCookie,
	)
	if configAudit.Code != http.StatusOK ||
		!strings.Contains(configAudit.Body.String(), member.ID) ||
		!strings.Contains(configAudit.Body.String(), "ServerName") ||
		!strings.Contains(configAudit.Body.String(), "成员修改后的服务器") {
		t.Fatalf("parameter audit response = %d %s", configAudit.Code, configAudit.Body.String())
	}
	configAuditData, err := os.ReadFile(cfg.ConfigAuditFile)
	if err != nil {
		t.Fatalf("read parameter audit file: %v", err)
	}
	if !strings.Contains(string(configAuditData), "PalWorldSettings.ini") {
		t.Fatalf("parameter audit file is missing source: %s", configAuditData)
	}

	allowedAction := requestForTest(
		t, handler, http.MethodPost, "/api/v1/games/palworld/actions",
		`{"action":"start"}`, memberCookie,
	)
	if allowedAction.Code != http.StatusAccepted {
		t.Fatalf("permitted action status = %d body = %s", allowedAction.Code, allowedAction.Body.String())
	}
	memberOverviewResponse := requestForTest(
		t, handler, http.MethodGet, "/api/v1/overview", "", memberCookie,
	)
	var memberOverview panel.Overview
	if err := json.Unmarshal(memberOverviewResponse.Body.Bytes(), &memberOverview); err != nil {
		t.Fatalf("decode member overview: %v", err)
	}
	if len(memberOverview.Activities) != 1 || memberOverview.Activities[0].Action != "start" {
		t.Fatalf("member activities = %#v; want only permitted control activity", memberOverview.Activities)
	}
	deniedAction := requestForTest(
		t, handler, http.MethodPost, "/api/v1/games/palworld/actions",
		`{"action":"update"}`, memberCookie,
	)
	if deniedAction.Code != http.StatusForbidden {
		t.Fatalf("unpermitted action status = %d body = %s", deniedAction.Code, deniedAction.Body.String())
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

	updated := requestForTest(
		t, handler, http.MethodPatch, "/api/v1/access/members/"+member.ID,
		`{"permissions":["game.backup"]}`, adminCookie,
	)
	if updated.Code != http.StatusOK {
		t.Fatalf("update permissions status = %d body = %s", updated.Code, updated.Body.String())
	}
	revokedAfterPermissionChange := requestForTest(
		t, handler, http.MethodGet, "/api/v1/overview", "", memberCookie,
	)
	if revokedAfterPermissionChange.Code != http.StatusUnauthorized {
		t.Fatalf("permission change session status = %d; want unauthorized", revokedAfterPermissionChange.Code)
	}

	memberCookie, _ = loginForTest(t, handler, "friend-secret-123", "198.51.100.22")
	backupMemberOverview := requestForTest(
		t, handler, http.MethodGet, "/api/v1/overview", "", memberCookie,
	)
	var filteredOverview panel.Overview
	if err := json.Unmarshal(backupMemberOverview.Body.Bytes(), &filteredOverview); err != nil {
		t.Fatalf("decode filtered member overview: %v", err)
	}
	if len(filteredOverview.Activities) != 0 {
		t.Fatalf("backup-only member activities = %#v; control activity must be filtered", filteredOverview.Activities)
	}
	settingsDenied := requestForTest(
		t, handler, http.MethodGet, "/api/v1/games/palworld/settings", "", memberCookie,
	)
	if settingsDenied.Code != http.StatusForbidden {
		t.Fatalf("settings after permission change status = %d; want forbidden", settingsDenied.Code)
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
	request.RemoteAddr = "127.0.0.1:12345"
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
	sessionCookie := cookieNamed(cookies, "hearth_session")
	if sessionCookie == nil {
		t.Fatalf("login cookies = %#v", cookies)
	}
	return sessionCookie, session.Role
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
