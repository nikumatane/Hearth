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
