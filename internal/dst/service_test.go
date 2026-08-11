package dst

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"hearth/internal/config"
	"hearth/internal/panel"
)

func testConfig(t *testing.T) config.GameConfig {
	t.Helper()
	root := t.TempDir()
	installDir := filepath.Join(root, "steamapps", "common", "Don't Starve Together Dedicated Server")
	clusterDir := filepath.Join(root, "Klei", "DoNotStarveTogether", "MyCluster")
	paths := []string{
		filepath.Join(installDir, "bin64", "dontstarve_dedicated_server_nullrenderer_x64.exe"),
		filepath.Join(clusterDir, "cluster.ini"), filepath.Join(clusterDir, "Master", "server.ini"),
		filepath.Join(clusterDir, "Caves", "server.ini"),
	}
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("server_port=11000\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return config.GameConfig{InstallDir: installDir, Executable: paths[0], ClusterDir: clusterDir, Port: 11000}
}

func TestNewServiceValidatesClusterBoundary(t *testing.T) {
	gameConfig := testConfig(t)
	service, err := NewService(gameConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	game, err := service.Game(gameID)
	if err != nil {
		t.Fatal(err)
	}
	if game.State != "stopped" || game.SaveID != "MyCluster" || game.Port != 11000 {
		t.Fatalf("game = %#v", game)
	}
	if _, err := service.Game("palworld"); !errors.Is(err, panel.ErrNotFound) {
		t.Fatalf("unknown game error = %v", err)
	}
}

func TestStopRequiresExplicitUnsafeConfirmation(t *testing.T) {
	service, err := NewService(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	service.masterRunning = true
	_, err = service.RunAction(gameID, panel.ActionRequest{Action: "stop"})
	if !errors.Is(err, panel.ErrUnsafe) || !strings.Contains(err.Error(), "REST") {
		t.Fatalf("stop error = %v", err)
	}
}

func TestStartRefusesMissingClusterToken(t *testing.T) {
	service, err := NewService(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	activity, err := service.RunAction(gameID, panel.ActionRequest{Action: "start"})
	if err != nil {
		t.Fatal(err)
	}
	if len(activity.Logs) != 2 || filepath.Ext(activity.Logs[0].ID) != ".log" || filepath.Ext(activity.Logs[1].ID) != ".log" {
		t.Fatalf("DST log refs = %#v", activity.Logs)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		activities := service.Overview().Activities
		if len(activities) > 0 && activities[0].ID == activity.ID && activities[0].Status == "error" {
			if !strings.Contains(activities[0].Detail, "cluster_token.txt") {
				t.Fatalf("start detail = %q", activities[0].Detail)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("start activity did not fail: %#v", service.Overview().Activities)
}

func TestUpdateClusterTokenWritesOnlyTokenFile(t *testing.T) {
	gameConfig := testConfig(t)
	service, err := NewService(gameConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()

	if service.ClusterTokenConfigured() {
		t.Fatal("token should initially be absent")
	}
	const token = "test-cluster-token-123"
	if err := service.UpdateClusterToken(token); err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(gameConfig.ClusterDir, "cluster_token.txt")
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	// Unix exposes the mode bits we request. Windows security is represented by
	// ACLs and does not map os.Chmod(0600) to a 0600 Mode().Perm() value.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("token file permissions = %o", info.Mode().Perm())
	}
	if string(data) != token || !service.ClusterTokenConfigured() {
		t.Fatalf("token file = %q, configured = %v", string(data), service.ClusterTokenConfigured())
	}
}

func TestUpdateClusterTokenRejectsInvalidOrRunning(t *testing.T) {
	service, err := NewService(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	for _, token := range []string{"", "bad\nvalue", "bad\x00value"} {
		if err := service.UpdateClusterToken(token); !errors.Is(err, panel.ErrInvalid) {
			t.Fatalf("token %q error = %v", token, err)
		}
	}
	service.masterRunning = true
	if err := service.UpdateClusterToken("valid-token"); !errors.Is(err, panel.ErrUnsafe) {
		t.Fatalf("running update error = %v", err)
	}
}

func TestReadINIIntFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.ini")
	if err := os.WriteFile(path, []byte("[NETWORK]\nserver_port = 11001\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readINIInt(path, "server_port", 11000); got != 11001 {
		t.Fatalf("port = %d", got)
	}
	if got := readINIInt(path, "missing", 11000); got != 11000 {
		t.Fatalf("fallback port = %d", got)
	}
}

func TestDSTConfigRevisionAndAtomicUpdate(t *testing.T) {
	gameConfig := testConfig(t)
	service, err := NewService(gameConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()

	document, err := service.DSTConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Files) != 3 || document.Revision == "" {
		t.Fatalf("DST config = %#v", document)
	}
	updated, err := service.UpdateDSTConfig(panel.DSTConfigPatch{
		Revision: document.Revision,
		Files:    map[string]string{"master": "server_port=11002\n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision == document.Revision || updated.Files[1].Content != "server_port=11002\n" {
		t.Fatalf("updated DST config = %#v", updated)
	}
	if _, err := service.UpdateDSTConfig(panel.DSTConfigPatch{Revision: document.Revision, Files: map[string]string{"master": "stale"}}); !errors.Is(err, panel.ErrInvalid) {
		t.Fatalf("stale revision error = %v", err)
	}
}

func TestDSTConfigRejectsRunningAndUnsafeContent(t *testing.T) {
	service, err := NewService(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	document, err := service.DSTConfig()
	if err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"bad\x00value", "not-an-ini-line", strings.Repeat("x", maxDSTConfigFileSize+1)} {
		_, err := service.UpdateDSTConfig(panel.DSTConfigPatch{Revision: document.Revision, Files: map[string]string{"cluster": content}})
		if !errors.Is(err, panel.ErrInvalid) {
			t.Fatalf("content error = %v", err)
		}
	}
	service.masterRunning = true
	_, err = service.UpdateDSTConfig(panel.DSTConfigPatch{Revision: document.Revision, Files: map[string]string{"cluster": "safe"}})
	if !errors.Is(err, panel.ErrUnsafe) {
		t.Fatalf("running update error = %v", err)
	}
}

func TestDSTSettingsStructuredUpdatePreservesUnknownContent(t *testing.T) {
	gameConfig := testConfig(t)
	clusterPath := filepath.Join(gameConfig.ClusterDir, "cluster.ini")
	clusterINI := "; keep this comment\n[NETWORK]\ncluster_name = Old name\ncluster_password = existing-secret\nunknown_mod_key = keep-me\n\n[GAMEPLAY]\nmax_players = 6\npvp = false\n"
	if err := os.WriteFile(clusterPath, []byte(clusterINI), 0o600); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(gameConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()

	document, err := service.DSTSettings()
	if err != nil {
		t.Fatal(err)
	}
	password := findDSTSetting(t, document, "cluster.network.cluster_password")
	if password.Value != dstSecretMask || !password.Sensitive || !password.Configured {
		t.Fatalf("password setting = %#v", password)
	}
	updated, err := service.UpdateDSTSettings(panel.DSTSettingsPatch{
		Revision: document.Revision,
		Changes: map[string]any{
			"cluster.network.cluster_name":      "New name",
			"cluster.gameplay.max_players":      float64(12),
			"cluster.gameplay.pause_when_empty": true,
			"cluster.network.cluster_password":  dstSecretMask,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision == document.Revision {
		t.Fatal("structured update did not change revision")
	}
	raw, err := os.ReadFile(clusterPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)
	for _, expected := range []string{"; keep this comment", "unknown_mod_key = keep-me", "cluster_name = New name", "max_players = 12", "pause_when_empty = true", "cluster_password = existing-secret"} {
		if !strings.Contains(content, expected) {
			t.Fatalf("updated cluster.ini missing %q:\n%s", expected, content)
		}
	}
}

func TestDSTSettingsRejectsUnknownStaleAndInvalidValues(t *testing.T) {
	service, err := NewService(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	document, err := service.DSTSettings()
	if err != nil {
		t.Fatal(err)
	}
	for _, changes := range []map[string]any{
		{"unknown.key": "value"},
		{"cluster.gameplay.max_players": float64(0)},
		{"cluster.network.cluster_name": "bad\nname"},
		{"caves.network.server_port": float64(10999)},
	} {
		_, err := service.UpdateDSTSettings(panel.DSTSettingsPatch{Revision: document.Revision, Changes: changes})
		if !errors.Is(err, panel.ErrInvalid) {
			t.Fatalf("changes %#v error = %v", changes, err)
		}
	}
	_, err = service.UpdateDSTSettings(panel.DSTSettingsPatch{Revision: "stale", Changes: map[string]any{"cluster.gameplay.pvp": true}})
	if !errors.Is(err, panel.ErrInvalid) {
		t.Fatalf("stale settings error = %v", err)
	}
}

func findDSTSetting(t *testing.T, document panel.DSTSettings, key string) panel.Setting {
	t.Helper()
	for _, group := range document.Groups {
		for _, setting := range group.Settings {
			if setting.Key == key {
				return setting
			}
		}
	}
	t.Fatalf("DST setting %s not found", key)
	return panel.Setting{}
}
