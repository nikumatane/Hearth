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
)

func TestOperationAuditRotatesAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operation-audit.jsonl")
	filler := bytes.Repeat([]byte("{}\n"), maxOperationAuditSize/3+1)
	if err := os.WriteFile(path, filler, 0o600); err != nil {
		t.Fatalf("seed operation audit: %v", err)
	}
	store := &operationAuditStore{path: path, entries: []operationAuditEntry{}}
	entry := operationAuditEntry{
		ID: "operation-1", Event: operationEventIPRuleAdded,
		ActorCredentialID: "ADMIN", ActorRole: roleAdmin, ActorIP: "101.68.35.123",
		TargetType: operationTargetIPRule, TargetID: "rule-1", TargetIP: "117.150.109.249",
		RuleKind: ipRuleAllow, Success: true, CreatedAt: time.Now(),
	}
	if err := store.record(entry); err != nil {
		t.Fatalf("record operation audit: %v", err)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("rotated operation audit not found: %v", err)
	}
	reloaded, err := newOperationAuditStore(path)
	if err != nil {
		t.Fatalf("reload operation audit: %v", err)
	}
	entries := reloaded.all()
	if len(entries) != 1 || entries[0].TargetIP != entry.TargetIP || entries[0].ActorIP != entry.ActorIP {
		t.Fatalf("reloaded operation audit = %#v", entries)
	}
}

func TestSecurityOperationsAreSeparateFromLoginAudit(t *testing.T) {
	directory := t.TempDir()
	cfg := config.Config{
		AdminPassword:      "admin-secret-123",
		AccessFile:         filepath.Join(directory, "member-credentials.json"),
		AuditFile:          filepath.Join(directory, "login-audit.jsonl"),
		OperationAuditFile: filepath.Join(directory, "operation-audit.jsonl"),
		IPRulesFile:        filepath.Join(directory, "ip-rules.json"),
	}
	handler := newTestHandler(t, cfg)
	adminCookie, _ := loginForTest(t, handler, cfg.AdminPassword, "203.0.113.11")

	createRuleRequest := httptest.NewRequest(
		http.MethodPost, "/api/v1/access/ip-rules",
		strings.NewReader(`{"ip":"117.150.109.249","kind":"allow"}`),
	)
	createRuleRequest.Header.Set("Content-Type", "application/json")
	createRuleRequest.RemoteAddr = "101.68.35.123:49152"
	createRuleRequest.AddCookie(adminCookie)
	createRuleResponse := httptest.NewRecorder()
	handler.ServeHTTP(createRuleResponse, createRuleRequest)
	if createRuleResponse.Code != http.StatusCreated {
		t.Fatalf("create rule status = %d body = %s", createRuleResponse.Code, createRuleResponse.Body.String())
	}

	loginAudit := requestForTest(t, handler, http.MethodGet, "/api/v1/access/audit", "", adminCookie)
	if loginAudit.Code != http.StatusOK || strings.Contains(loginAudit.Body.String(), operationEventIPRuleAdded) ||
		strings.Contains(loginAudit.Body.String(), "117.150.109.249") {
		t.Fatalf("login audit contains a security operation: %d %s", loginAudit.Code, loginAudit.Body.String())
	}

	operationAudit := requestForTest(
		t, handler, http.MethodGet, "/api/v1/access/operation-audit", "", adminCookie,
	)
	var body struct {
		Entries []operationAuditEntry `json:"entries"`
	}
	if err := json.Unmarshal(operationAudit.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode operation audit: %v", err)
	}
	if operationAudit.Code != http.StatusOK || len(body.Entries) != 1 {
		t.Fatalf("operation audit response = %d %#v", operationAudit.Code, body.Entries)
	}
	entry := body.Entries[0]
	if entry.ActorIP != "101.68.35.123" || entry.TargetIP != "117.150.109.249" ||
		entry.ActorIP == entry.TargetIP || entry.Event != operationEventIPRuleAdded {
		t.Fatalf("operation actor/target separation = %#v", entry)
	}
}

func TestMemberMutationsAreSecurityOperationsWithoutPasswords(t *testing.T) {
	directory := t.TempDir()
	cfg := config.Config{
		AdminPassword:      "admin-secret-123",
		AccessFile:         filepath.Join(directory, "member-credentials.json"),
		OperationAuditFile: filepath.Join(directory, "operation-audit.jsonl"),
	}
	handler := newTestHandler(t, cfg)
	adminCookie, _ := loginForTest(t, handler, cfg.AdminPassword, "203.0.113.11")

	create := requestForTest(
		t, handler, http.MethodPost, "/api/v1/access/members",
		`{"password":"friend-secret-123","permissions":["game.control"]}`, adminCookie,
	)
	var member memberView
	if create.Code != http.StatusCreated || json.Unmarshal(create.Body.Bytes(), &member) != nil {
		t.Fatalf("create member response = %d %s", create.Code, create.Body.String())
	}
	update := requestForTest(
		t, handler, http.MethodPatch, "/api/v1/access/members/"+member.ID,
		`{"password":"friend-secret-456","permissions":["game.backup"]}`, adminCookie,
	)
	if update.Code != http.StatusOK {
		t.Fatalf("update member response = %d %s", update.Code, update.Body.String())
	}
	deleted := requestForTest(
		t, handler, http.MethodDelete, "/api/v1/access/members/"+member.ID, "", adminCookie,
	)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete member response = %d %s", deleted.Code, deleted.Body.String())
	}

	response := requestForTest(
		t, handler, http.MethodGet, "/api/v1/access/operation-audit", "", adminCookie,
	)
	var body struct {
		Entries []operationAuditEntry `json:"entries"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Entries) != 3 || body.Entries[0].Event != operationEventMemberDeleted ||
		body.Entries[1].Event != operationEventMemberUpdated ||
		!body.Entries[1].PasswordChanged || !body.Entries[1].PermissionsChanged ||
		body.Entries[2].Event != operationEventMemberCreated {
		t.Fatalf("member operation audit = %#v", body.Entries)
	}
	data, err := os.ReadFile(cfg.OperationAuditFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "friend-secret-123") ||
		strings.Contains(string(data), "friend-secret-456") ||
		strings.Contains(string(data), "admin-secret-123") {
		t.Fatalf("operation audit contains a plaintext password: %s", data)
	}
}

func TestLoginAuditFiltersLegacyRuleOperations(t *testing.T) {
	store := &accessStore{audits: []auditEntry{
		{ID: "operation", Event: operationEventIPRuleAdded, CreatedAt: time.Now()},
		{ID: "login", Event: "login", CreatedAt: time.Now()},
	}}
	entries := store.auditEntries()
	if len(entries) != 1 || entries[0].ID != "login" {
		t.Fatalf("filtered login audit = %#v", entries)
	}
}
