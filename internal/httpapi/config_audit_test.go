package httpapi

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hearth/internal/panel"
)

func TestBuildConfigAuditRedactsSensitiveValues(t *testing.T) {
	before := panel.PalworldSettings{
		Revision: "before",
		Groups: []panel.SettingGroup{{Settings: []panel.Setting{
			{Key: "ServerName", Label: "服务器名称", Value: "旧名称"},
			{Key: "AdminPassword", Label: "管理员密码", Value: "••••••••", Sensitive: true},
		}}},
	}
	after := panel.PalworldSettings{
		Revision: "after",
		Groups: []panel.SettingGroup{{Settings: []panel.Setting{
			{Key: "ServerName", Label: "服务器名称", Value: "新名称"},
			{Key: "AdminPassword", Label: "管理员密码", Value: "••••••••", Sensitive: true},
		}}},
	}
	entry, ok := buildConfigAuditEntry(
		before,
		after,
		panel.PalworldSettingsPatch{Changes: map[string]any{
			"ServerName": "新名称", "AdminPassword": "never-persist-this-secret",
		}},
		principal{Role: roleAdmin, CredentialID: "ADMIN"},
		"127.0.0.1",
	)
	if !ok || len(entry.Changes) != 2 {
		t.Fatalf("audit entry = %#v, %v", entry, ok)
	}
	for _, change := range entry.Changes {
		if change.Key == "AdminPassword" &&
			(!change.Sensitive || change.Before != "" || change.After != "") {
			t.Fatalf("sensitive change was not redacted: %#v", change)
		}
	}

	path := filepath.Join(t.TempDir(), "config-audit.jsonl")
	store, err := newConfigAuditStore(path)
	if err != nil {
		t.Fatalf("create parameter audit store: %v", err)
	}
	if err := store.record(entry); err != nil {
		t.Fatalf("record parameter audit: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read parameter audit: %v", err)
	}
	if strings.Contains(string(data), "never-persist-this-secret") {
		t.Fatalf("parameter audit contains sensitive plaintext: %s", data)
	}
}

func TestConfigAuditStoreRotatesAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config-audit.jsonl")
	store, err := newConfigAuditStore(path)
	if err != nil {
		t.Fatalf("create parameter audit store: %v", err)
	}
	filler := bytes.Repeat([]byte("{}\n"), maxConfigAuditSize/3+1)
	if err := os.WriteFile(path, filler, 0o600); err != nil {
		t.Fatalf("seed parameter audit: %v", err)
	}
	entry := configAuditEntry{
		ID: "audit-1", GameID: "palworld", Source: "PalWorldSettings.ini",
		CredentialID: "ADMIN", Role: roleAdmin, IP: "127.0.0.1",
		RevisionBefore: "before", RevisionAfter: "after",
		Changes:   []configAuditChange{{Key: "ExpRate", Label: "经验倍率", Before: "1", After: "2"}},
		CreatedAt: time.Now(),
	}
	if err := store.record(entry); err != nil {
		t.Fatalf("record rotated parameter audit: %v", err)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("rotated parameter audit not found: %v", err)
	}
	reloaded, err := newConfigAuditStore(path)
	if err != nil {
		t.Fatalf("reload parameter audit store: %v", err)
	}
	entries := reloaded.all()
	if len(entries) != 1 || entries[0].ID != entry.ID {
		t.Fatalf("reloaded entries = %#v", entries)
	}
}
