package gamemanager

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"hearth/internal/config"
	"hearth/internal/dst"
	"hearth/internal/panel"
)

func TestCreateDSTClusterWritesCompletePrivateConfiguration(t *testing.T) {
	root := t.TempDir()
	plan := dstInstallPlan{
		clusterDir:   filepath.Join(root, "HearthCluster"),
		clusterName:  "Hearth 测试服",
		clusterToken: "sensitive-klei-token",
	}
	rollback, err := createDSTCluster(plan)
	if err != nil {
		t.Fatalf("createDSTCluster() error = %v", err)
	}
	t.Cleanup(func() { _ = rollback() })
	for _, relative := range []string{
		"cluster.ini", "cluster_token.txt", filepath.Join("Master", "server.ini"),
		filepath.Join("Caves", "server.ini"), filepath.Join("Caves", "worldgenoverride.lua"),
	} {
		info, err := os.Stat(filepath.Join(plan.clusterDir, relative))
		if err != nil {
			t.Fatalf("stat %s: %v", relative, err)
		}
		// Windows exposes access through NTFS ACLs rather than POSIX mode bits;
		// os.FileMode therefore reports regular files as 0666 there even when
		// the inherited ACL is private to the service account.
		if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("%s mode = %o, want private", relative, info.Mode().Perm())
		}
	}
	clusterINI, err := os.ReadFile(filepath.Join(plan.clusterDir, "cluster.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(clusterINI, []byte("cluster_name = Hearth 测试服")) ||
		!bytes.Contains(clusterINI, []byte("cluster_key = ")) ||
		bytes.Contains(clusterINI, []byte(plan.clusterToken)) {
		t.Fatalf("cluster.ini = %q", clusterINI)
	}
	token, err := os.ReadFile(filepath.Join(plan.clusterDir, "cluster_token.txt"))
	if err != nil || string(token) != plan.clusterToken {
		t.Fatalf("cluster token = %q, error = %v", token, err)
	}
}

func TestCreateDSTClusterOmitsOptionalToken(t *testing.T) {
	clusterDir := filepath.Join(t.TempDir(), "HearthCluster")
	rollback, err := createDSTCluster(dstInstallPlan{clusterDir: clusterDir, clusterName: "No Token"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rollback() })
	if _, err := os.Stat(filepath.Join(clusterDir, "cluster_token.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("optional token file error = %v", err)
	}
}

func TestDSTInstallValidationRejectsExistingUnsafeAndInjectedInputs(t *testing.T) {
	root := t.TempDir()
	steamRoot := filepath.Join(root, "steamcmd")
	clusterParent := filepath.Join(root, "Documents", "Klei", "DoNotStarveTogether")
	if err := os.MkdirAll(clusterParent, 0o700); err != nil {
		t.Fatal(err)
	}
	base := panel.InstallGameRequest{
		SteamCmdRoot: steamRoot,
		DST: &panel.DSTInstallOptions{
			ClusterDir: filepath.Join(clusterParent, "HearthCluster"), ClusterName: "Hearth",
		},
	}
	if _, err := newDSTInstallPlan(base, filepath.Join(root, "state", "config.json")); err != nil {
		t.Fatalf("newDSTInstallPlan() error = %v", err)
	}

	existing := base
	if err := os.MkdirAll(existing.DST.ClusterDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := newDSTInstallPlan(existing, filepath.Join(root, "state", "config.json")); !errors.Is(err, panel.ErrInvalid) {
		t.Fatalf("existing cluster error = %v", err)
	}
	if err := os.Remove(existing.DST.ClusterDir); err != nil {
		t.Fatal(err)
	}

	injected := base
	injected.DST = &panel.DSTInstallOptions{ClusterDir: base.DST.ClusterDir, ClusterName: "bad\n[SHARD]"}
	if _, err := newDSTInstallPlan(injected, filepath.Join(root, "state", "config.json")); !errors.Is(err, panel.ErrInvalid) {
		t.Fatalf("injected cluster name error = %v", err)
	}

	overlap := base
	overlap.DST = &panel.DSTInstallOptions{ClusterDir: filepath.Join(steamRoot, "cluster"), ClusterName: "bad path"}
	if _, err := newDSTInstallPlan(overlap, filepath.Join(root, "state", "config.json")); !errors.Is(err, panel.ErrInvalid) {
		t.Fatalf("overlapping cluster path error = %v", err)
	}
}

func TestDSTInstallValidationRejectsSymlinkResolvedOverlap(t *testing.T) {
	root := t.TempDir()
	steamRoot := filepath.Join(root, "steamcmd")
	if err := os.MkdirAll(steamRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	linkedParent := filepath.Join(root, "linked-clusters")
	if err := os.Symlink(steamRoot, linkedParent); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	err := validateNewDSTClusterPath(
		filepath.Join(linkedParent, "HearthCluster"), steamRoot, dstInstallDir(steamRoot), filepath.Join(root, "state", "config.json"),
	)
	if !errors.Is(err, panel.ErrInvalid) || !strings.Contains(err.Error(), "真实路径") {
		t.Fatalf("symlink overlap error = %v", err)
	}
}

func TestExistingDSTInstallRequiresMatchingSteamManifest(t *testing.T) {
	root := t.TempDir()
	installDir := dstInstallDir(root)
	executable := filepath.Join(installDir, "bin64", "dontstarve_dedicated_server_nullrenderer_x64.exe")
	if err := os.MkdirAll(filepath.Dir(executable), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateExistingDSTInstall(root, installDir); !errors.Is(err, panel.ErrInvalid) {
		t.Fatalf("missing manifest error = %v", err)
	}
	manifest := filepath.Join(root, "steamapps", "appmanifest_343050.acf")
	if err := os.WriteFile(manifest, []byte("\"AppState\"\n{\n\t\"appid\"\t\t\"343050\"\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateExistingDSTInstall(root, installDir); err != nil {
		t.Fatalf("valid install error = %v", err)
	}
}

func TestDSTActivationFailureRollsBackOnlyNewClusterAndDoesNotPersistToken(t *testing.T) {
	root := t.TempDir()
	steamRoot := filepath.Join(root, "steamcmd")
	installDir := dstInstallDir(steamRoot)
	for path, contents := range map[string]string{
		filepath.Join(installDir, "bin64", "dontstarve_dedicated_server_nullrenderer_x64.exe"): "test",
		filepath.Join(steamRoot, "steamapps", "appmanifest_343050.acf"):                        "\"AppState\"\n{\n\t\"appid\"\t\t\"343050\"\n}\n",
		filepath.Join(steamRoot, "steamcmd.exe"):                                               "test",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	clusterParent := filepath.Join(root, "clusters")
	if err := os.MkdirAll(clusterParent, 0o700); err != nil {
		t.Fatal(err)
	}
	clusterDir := filepath.Join(clusterParent, "HearthCluster")
	const token = "must-not-leak"
	service := &Service{
		configPath: filepath.Join(root, "state", "config.json"),
		candidates: map[string][]panel.GameCandidate{}, logPaths: map[string]string{},
		installing: true, installingGameID: dstID, activeTask: "install-test",
		activities: []panel.Activity{{ID: "install-test", Status: "running"}},
		dstFactory: func(gameConfig config.GameConfig) (panel.Service, error) {
			if !validDSTCluster(gameConfig.ClusterDir) {
				t.Fatal("factory received an incomplete cluster")
			}
			return panel.NewDemoService(), nil
		},
		saveConfig: func(_ string, cfg config.Config) error {
			encoded, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(encoded, []byte(token)) {
				t.Fatal("cluster token leaked into Hearth configuration")
			}
			return errors.New("simulated persistence failure")
		},
	}
	plan := gameInstallPlan{
		gameID: dstID, installDir: installDir, steamCmdRoot: steamRoot,
		dst: &dstInstallPlan{steamCmdRoot: steamRoot, installDir: installDir, clusterDir: clusterDir, clusterName: "Hearth", clusterToken: token},
	}
	var failed error
	service.finishDSTInstall("install-test", plan, filepath.Join(steamRoot, "steamcmd.exe"), func(string, int, string) {}, func(err error) {
		failed = err
		service.finishInstallActivity("install-test", false, "安装失败", safeErrorDetail(err))
	})
	if failed == nil {
		t.Fatal("expected activation failure")
	}
	if _, err := os.Stat(clusterDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new cluster was not rolled back: %v", err)
	}
	if !fileExists(filepath.Join(installDir, "bin64", "dontstarve_dedicated_server_nullrenderer_x64.exe")) {
		t.Fatal("Steam installation was unexpectedly removed")
	}
}

func TestDSTInstallActivationPersistsOnlyPathsAndKeepsServerStopped(t *testing.T) {
	root := t.TempDir()
	steamRoot := filepath.Join(root, "steamcmd")
	installDir := dstInstallDir(steamRoot)
	manifest := "\"AppState\"\n{\n\t\"appid\"\t\t\"343050\"\n}\n"
	for path, contents := range map[string]string{
		filepath.Join(installDir, "bin64", "dontstarve_dedicated_server_nullrenderer_x64.exe"): "test",
		filepath.Join(steamRoot, "steamapps", "appmanifest_343050.acf"):                        manifest,
		filepath.Join(steamRoot, "steamcmd.exe"):                                               "test",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	clusterParent := filepath.Join(root, "Documents", "Klei", "DoNotStarveTogether")
	if err := os.MkdirAll(clusterParent, 0o700); err != nil {
		t.Fatal(err)
	}
	clusterDir := filepath.Join(clusterParent, "HearthCluster")
	const token = "one-time-install-token"
	var persisted []byte
	service := &Service{
		configPath: filepath.Join(root, "state", "config.json"),
		candidates: map[string][]panel.GameCandidate{}, logPaths: map[string]string{},
		installing: true, installingGameID: dstID, activeTask: "install-test",
		activities: []panel.Activity{{ID: "install-test", Status: "running"}},
		dstFactory: func(gameConfig config.GameConfig) (panel.Service, error) {
			if !validDSTCluster(gameConfig.ClusterDir) {
				t.Fatal("factory received an incomplete cluster")
			}
			return dst.NewService(gameConfig)
		},
		saveConfig: func(_ string, cfg config.Config) error {
			var err error
			persisted, err = json.Marshal(cfg)
			return err
		},
	}
	plan := gameInstallPlan{
		gameID: dstID, installDir: installDir, steamCmdRoot: steamRoot,
		dst: &dstInstallPlan{steamCmdRoot: steamRoot, installDir: installDir, clusterDir: clusterDir, clusterName: "Hearth", clusterToken: token},
	}
	var failed error
	service.finishDSTInstall("install-test", plan, filepath.Join(steamRoot, "steamcmd.exe"), func(string, int, string) {}, func(err error) { failed = err })
	if failed != nil {
		t.Fatalf("finishDSTInstall() error = %v", failed)
	}
	if bytes.Contains(persisted, []byte(token)) {
		t.Fatal("cluster token leaked into persisted Hearth configuration")
	}
	if service.dstDelegate == nil || service.installing || service.activeTask != "" {
		t.Fatalf("service activation state = delegate %v, installing %v, task %q", service.dstDelegate != nil, service.installing, service.activeTask)
	}
	t.Cleanup(func() { closeService(service.dstDelegate) })
	game, err := service.dstDelegate.Game(dstID)
	if err != nil || game.State != "stopped" {
		t.Fatalf("new DST runtime state = %q, error = %v", game.State, err)
	}
	if got := service.activities[0]; got.Status != "success" || got.Progress != 100 {
		t.Fatalf("activity = %#v", got)
	}
	data, err := os.ReadFile(filepath.Join(clusterDir, "cluster_token.txt"))
	if err != nil || string(data) != token {
		t.Fatalf("cluster token = %q, error = %v", data, err)
	}
	if state := service.Management().Games[1].State; state != "managed" {
		t.Fatalf("DST management state = %q", state)
	}
}
