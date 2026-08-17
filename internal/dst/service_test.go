package dst

import (
	"archive/zip"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"hearth/internal/config"
	"hearth/internal/panel"
)

const dstSteamFixtureName = "dst-valve-client"

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
	if !game.UpdateSupported || !game.BackupSupported || !game.BackupRequiresStopped ||
		!game.UpdateRequiresUnsafeStop || game.VersionSource != "Steam appmanifest" {
		t.Fatalf("DST capabilities = %#v", game)
	}
	if _, err := service.Game("palworld"); !errors.Is(err, panel.ErrNotFound) {
		t.Fatalf("unknown game error = %v", err)
	}
}

func TestCheckVersionUsesDSTDedicatedServerAppWithoutUpdatingFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses a POSIX shell script")
	}
	gameConfig := testConfig(t)
	steamRoot := filepath.Dir(filepath.Dir(filepath.Dir(gameConfig.InstallDir)))
	gameConfig.SteamCmd = filepath.Join(steamRoot, dstSteamFixtureName)
	manifestPath := filepath.Join(steamRoot, "steamapps", "appmanifest_"+dstAppID+".acf")
	manifest := `"AppState"
{
	"buildid" "100"
	"MountedDepots"
	{
		"343051" "700"
	}
}`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	attemptsPath := filepath.Join(steamRoot, "attempts")
	script := fmt.Sprintf(`#!/bin/sh
count=0
if [ -f %q ]; then count=$(cat %q); fi
count=$((count + 1))
printf "%%s" "$count" > %q
case "$*" in
  *app_info_print*)
    printf '"343050"\n{\n"depots"\n{\n"343051"\n{\n"manifests"\n{\n"public"\n{\n"gid" "701"\n}\n}\n}\n"branches"\n{\n"public"\n{\n"buildid" "101"\n}\n}\n}\n}\n'
    ;;
esac
`, attemptsPath, attemptsPath, attemptsPath)
	if err := os.WriteFile(gameConfig.SteamCmd, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(gameConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	logID := "dst-version-test.log"
	if err := service.checkVersion(func(string, int, string) {}, logID); err != nil {
		t.Fatal(err)
	}
	attempts, err := os.ReadFile(attemptsPath)
	if err != nil || string(attempts) != "2" {
		t.Fatalf("SteamCMD attempts = %q, error = %v", attempts, err)
	}
	game, err := service.Game(gameID)
	if err != nil {
		t.Fatal(err)
	}
	if !game.UpdateAvailable || game.AvailableVersion != "101" || !game.UpdateSupported {
		t.Fatalf("DST version status = %#v", game)
	}
}

func TestDSTSteamVersionCommandStopsAfterNoLogProgress(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses a POSIX shell script")
	}
	gameConfig := testConfig(t)
	gameConfig.SteamCmd = filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(gameConfig.InstallDir))), dstSteamFixtureName)
	if err := os.WriteFile(gameConfig.SteamCmd, []byte("#!/bin/sh\nsleep 10\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(gameConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	logPath := filepath.Join(t.TempDir(), "steamcmd.log")
	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	started := time.Now()
	err = service.runSteamVersionCommand(logFile, 150*time.Millisecond, "+quit")
	if err == nil || !strings.Contains(err.Error(), "no log progress") {
		t.Fatalf("runSteamVersionCommand() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("no-progress termination took %v", elapsed)
	}
}

func TestDSTSteamUpdateResultRequiresTargetAppCompletion(t *testing.T) {
	directory := t.TempDir()
	for name, fixture := range map[string]struct {
		log       string
		confirmed bool
	}{
		"updated":  {log: "Success! App '343050' fully installed.", confirmed: true},
		"current":  {log: "Success! App '343050' already up to date.", confirmed: true},
		"palworld": {log: "Success! App '2394010' fully installed.", confirmed: false},
		"steamcmd": {log: "Loading Steam API...OK", confirmed: false},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(directory, name+".log")
			if err := os.WriteFile(path, []byte(fixture.log), 0o600); err != nil {
				t.Fatal(err)
			}
			_, confirmed := dstSteamUpdateResult(path)
			if confirmed != fixture.confirmed {
				t.Fatalf("dstSteamUpdateResult(%q) confirmed = %v", fixture.log, confirmed)
			}
		})
	}
}

func TestDSTBackupArchivesStoppedClusterAndExcludesPanelState(t *testing.T) {
	gameConfig := testConfig(t)
	savePath := filepath.Join(gameConfig.ClusterDir, "Master", "save", "session", "world.dat")
	if err := os.MkdirAll(filepath.Dir(savePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(savePath, []byte("world-state"), 0o600); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(gameConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if err := os.MkdirAll(filepath.Join(service.config.ClusterDir, "panel-logs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(service.config.ClusterDir, "panel-logs", "ignore.log"), []byte("ignore"), 0o600); err != nil {
		t.Fatal(err)
	}
	backupPath, err := service.createBackup(func(string, int, string) {}, "dst-backup-test.log")
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.OpenReader(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	foundSave := false
	for _, file := range archive.File {
		if strings.Contains(file.Name, "panel-backups") || strings.Contains(file.Name, "panel-logs") {
			t.Fatalf("panel state leaked into cluster backup: %s", file.Name)
		}
		if file.Name == "Master/save/session/world.dat" {
			foundSave = true
		}
	}
	if !foundSave {
		t.Fatalf("save file missing from backup %s", backupPath)
	}
}

func TestDSTBackupRequiresStoppedShards(t *testing.T) {
	service, err := NewService(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	service.masterRunning = true
	if _, err := service.RunAction(gameID, panel.ActionRequest{Action: "backup"}); !errors.Is(err, panel.ErrUnsafe) {
		t.Fatalf("running backup error = %v", err)
	}
}

func TestDSTBackupRetentionUsesAgeAndCapacityAndKeepsCurrent(t *testing.T) {
	service, err := NewService(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	service.config.BackupRetentionDays = 30
	service.config.BackupMaxTotalGB = 1
	if err := os.MkdirAll(service.config.BackupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	type fixture struct {
		name string
		size int64
		age  time.Duration
	}
	fixtures := []fixture{
		{name: "dst-backup-current.zip", size: 1, age: 0},
		{name: "dst-backup-new.zip", size: 600 << 20, age: time.Hour},
		{name: "dst-backup-capacity.zip", size: 600 << 20, age: 2 * time.Hour},
		{name: "dst-backup-expired.zip", size: 1, age: 40 * 24 * time.Hour},
	}
	for _, item := range fixtures {
		path := filepath.Join(service.config.BackupDir, item.name)
		file, createErr := os.Create(path)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if truncateErr := file.Truncate(item.size); truncateErr != nil {
			_ = file.Close()
			t.Fatal(truncateErr)
		}
		if closeErr := file.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
		if err := os.Chtimes(path, now.Add(-item.age), now.Add(-item.age)); err != nil {
			t.Fatal(err)
		}
	}
	unknown := filepath.Join(service.config.BackupDir, "manual-backup.zip")
	if err := os.WriteFile(unknown, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(service.config.BackupDir, "dst-backup-current.zip")
	removed, _, err := service.pruneBackups(current, now)
	if err != nil || removed != 2 {
		t.Fatalf("prune removed=%d error=%v", removed, err)
	}
	for _, name := range []string{"dst-backup-current.zip", "dst-backup-new.zip", "manual-backup.zip"} {
		if _, err := os.Stat(filepath.Join(service.config.BackupDir, name)); err != nil {
			t.Fatalf("retained backup %s: %v", name, err)
		}
	}
	for _, name := range []string{"dst-backup-capacity.zip", "dst-backup-expired.zip"} {
		if _, err := os.Stat(filepath.Join(service.config.BackupDir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("backup %s was not removed: %v", name, err)
		}
	}
}

func TestDSTUpdateStopsBacksUpUpdatesAndRestoresShards(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses POSIX shell scripts")
	}
	gameConfig := testConfig(t)
	if err := os.WriteFile(gameConfig.Executable, []byte("#!/bin/sh\nsleep 30\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(gameConfig.Executable, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gameConfig.ClusterDir, "cluster_token.txt"), []byte("test-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	steamRoot := filepath.Dir(filepath.Dir(filepath.Dir(gameConfig.InstallDir)))
	gameConfig.SteamCmd = filepath.Join(steamRoot, dstSteamFixtureName)
	manifestPath := filepath.Join(steamRoot, "steamapps", "appmanifest_"+dstAppID+".acf")
	manifest := `"AppState"
{
	"buildid" "100"
	"MountedDepots"
	{
		"343051" "700"
	}
}`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	steamScript := `#!/bin/sh
case "$*" in
  *app_update*) printf " Update state (0x61) downloading, progress: 100.00 (1 / 1)\nSuccess! App '343050' fully installed.\n" ;;
esac
`
	if err := os.WriteFile(gameConfig.SteamCmd, []byte(steamScript), 0o700); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(gameConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	previousHealthDuration := shardHealthConfirmationDuration
	previousRecoveryDuration := recoveryHealthDuration
	shardHealthConfirmationDuration = 200 * time.Millisecond
	recoveryHealthDuration = 200 * time.Millisecond
	defer func() {
		shardHealthConfirmationDuration = previousHealthDuration
		recoveryHealthDuration = previousRecoveryDuration
	}()

	start, err := service.RunAction(gameID, panel.ActionRequest{Action: "start"})
	if err != nil {
		t.Fatal(err)
	}
	waitDSTActivity(t, service, start.ID, "success", 3*time.Second)
	if _, err := service.RunAction(gameID, panel.ActionRequest{Action: "update"}); !errors.Is(err, panel.ErrUnsafe) {
		t.Fatalf("running update without confirmation error = %v", err)
	}
	update, err := service.RunAction(gameID, panel.ActionRequest{Action: "update", AllowUnsafe: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(update.Logs) != 3 {
		t.Fatalf("update logs = %#v", update.Logs)
	}
	waitDSTActivity(t, service, update.ID, "success", 8*time.Second)
	game, err := service.Game(gameID)
	if err != nil {
		t.Fatal(err)
	}
	if game.State != "running" || game.VersionCheck != versionCheckCurrent || game.LastBackupAt == nil {
		t.Fatalf("game after update = %#v", game)
	}
	backups, err := filepath.Glob(filepath.Join(service.config.BackupDir, "dst-backup-*.zip"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups = %#v, error = %v", backups, err)
	}
	stop, err := service.RunAction(gameID, panel.ActionRequest{Action: "stop", AllowUnsafe: true})
	if err != nil {
		t.Fatal(err)
	}
	waitDSTActivity(t, service, stop.ID, "success", 3*time.Second)
	stoppedUpdate, err := service.RunAction(gameID, panel.ActionRequest{Action: "update"})
	if err != nil {
		t.Fatal(err)
	}
	waitDSTActivity(t, service, stoppedUpdate.ID, "success", 5*time.Second)
	stoppedGame, err := service.Game(gameID)
	if err != nil {
		t.Fatal(err)
	}
	if stoppedGame.State != "stopped" {
		t.Fatalf("stopped server was unexpectedly started: %#v", stoppedGame)
	}
}

func TestDSTUpdateFailureRetainsBackupAndRestoresOriginalRuntime(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses POSIX shell scripts")
	}
	gameConfig := testConfig(t)
	if err := os.WriteFile(gameConfig.Executable, []byte("#!/bin/sh\nsleep 30\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(gameConfig.Executable, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gameConfig.ClusterDir, "cluster_token.txt"), []byte("test-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	steamRoot := filepath.Dir(filepath.Dir(filepath.Dir(gameConfig.InstallDir)))
	gameConfig.SteamCmd = filepath.Join(steamRoot, dstSteamFixtureName)
	manifestPath := filepath.Join(steamRoot, "steamapps", "appmanifest_"+dstAppID+".acf")
	if err := os.WriteFile(manifestPath, []byte(`"AppState"
{
	"buildid" "100"
	"MountedDepots" { "343051" "700" }
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gameConfig.SteamCmd, []byte("#!/bin/sh\ncase \"$*\" in *app_update*) echo \"Error! App '343050' state is 0x6 after update job.\"; exit 1;; esac\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(gameConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	previousRecoveryDuration := recoveryHealthDuration
	recoveryHealthDuration = 200 * time.Millisecond
	defer func() { recoveryHealthDuration = previousRecoveryDuration }()
	start, err := service.RunAction(gameID, panel.ActionRequest{Action: "start"})
	if err != nil {
		t.Fatal(err)
	}
	waitDSTActivity(t, service, start.ID, "success", 3*time.Second)
	update, err := service.RunAction(gameID, panel.ActionRequest{Action: "update", AllowUnsafe: true})
	if err != nil {
		t.Fatal(err)
	}
	failed := waitDSTActivity(t, service, update.ID, "error", 8*time.Second)
	if !strings.Contains(failed.Detail, "0x6") && !strings.Contains(failed.Detail, "exit status") {
		t.Fatalf("failure detail = %q", failed.Detail)
	}
	game, err := service.Game(gameID)
	if err != nil {
		t.Fatal(err)
	}
	if game.State != "running" || game.LastBackupAt == nil {
		t.Fatalf("runtime was not restored after update failure: %#v", game)
	}
	stop, err := service.RunAction(gameID, panel.ActionRequest{Action: "stop", AllowUnsafe: true})
	if err != nil {
		t.Fatal(err)
	}
	waitDSTActivity(t, service, stop.ID, "success", 3*time.Second)
}

func waitDSTActivity(t *testing.T, service *Service, id, status string, timeout time.Duration) panel.Activity {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, activity := range service.Overview().Activities {
			if activity.ID != id || activity.Status == "running" {
				continue
			}
			if activity.Status != status {
				t.Fatalf("activity %s = %#v", id, activity)
			}
			return activity
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("activity %s did not reach %s: %#v", id, status, service.Overview().Activities)
	return panel.Activity{}
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
	if len(document.Files) != 5 || document.Revision == "" {
		t.Fatalf("DST config = %#v", document)
	}
	if document.Files[3].Exists || document.Files[4].Exists {
		t.Fatalf("optional worldgen files unexpectedly exist: %#v", document.Files[3:])
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

func TestDSTWorldSettingsCreatesStaticOverrideWithoutTouchingSave(t *testing.T) {
	gameConfig := testConfig(t)
	savePath := filepath.Join(gameConfig.ClusterDir, "Master", "save", "session", "marker")
	if err := os.MkdirAll(filepath.Dir(savePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(savePath, []byte("keep-save"), 0o600); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(gameConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()

	document, err := service.DSTWorldSettings()
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Shards) != 2 || document.Shards[0].Configured || document.Shards[1].Configured {
		t.Fatalf("initial world settings = %#v", document)
	}
	setting := findDSTWorldSetting(t, document, "master.world.world_size")
	if setting.ApplyMode != "regenerate" || setting.Value != "default" {
		t.Fatalf("world size setting = %#v", setting)
	}
	updated, err := service.UpdateDSTWorldSettings(panel.DSTWorldSettingsPatch{
		Revision: document.Revision,
		Changes:  map[string]any{"master.world.world_size": "huge", "master.world.hounds": "rare"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Shards[0].Configured || updated.Shards[1].Configured {
		t.Fatalf("updated shards = %#v", updated.Shards)
	}
	content, err := os.ReadFile(filepath.Join(gameConfig.ClusterDir, "Master", "worldgenoverride.lua"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`world_size = "huge"`, `hounds = "rare"`} {
		if !strings.Contains(string(content), expected) {
			t.Fatalf("worldgenoverride.lua missing %q:\n%s", expected, content)
		}
	}
	if _, err := parseDSTWorldgen(string(content)); err != nil {
		t.Fatalf("written worldgenoverride.lua does not round trip: %v", err)
	}
	marker, err := os.ReadFile(savePath)
	if err != nil || string(marker) != "keep-save" {
		t.Fatalf("existing save changed: content=%q err=%v", marker, err)
	}
}

func TestDSTWorldSettingsPreservesUnknownContentAndRejectsUnsafeLua(t *testing.T) {
	gameConfig := testConfig(t)
	path := filepath.Join(gameConfig.ClusterDir, "Master", "worldgenoverride.lua")
	content := "-- keep this comment\nreturn {\n    override_enabled = true,\n    preset = \"SURVIVAL_TOGETHER\",\n    custom_static = { enabled = true, },\n    overrides = {\n        world_size = \"small\", -- keep inline\n        future_rule = \"always\",\n    },\n}\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(gameConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	document, err := service.DSTWorldSettings()
	if err != nil {
		t.Fatal(err)
	}
	updated, err := service.UpdateDSTWorldSettings(panel.DSTWorldSettingsPatch{Revision: document.Revision, Changes: map[string]any{"master.world.world_size": "medium"}})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision == document.Revision {
		t.Fatal("world settings revision did not change")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"-- keep this comment", "-- keep inline", `future_rule = "always"`, `world_size = "medium"`, "custom_static"} {
		if !strings.Contains(string(raw), expected) {
			t.Fatalf("updated worldgen missing %q:\n%s", expected, raw)
		}
	}
	config, err := service.DSTConfig()
	if err != nil {
		t.Fatal(err)
	}
	for _, unsafe := range []string{
		`return os.execute("whoami")`,
		`return { overrides = require("evil") }`,
		`return { overrides = { world_size = (function() return "huge" end)() } }`,
	} {
		_, err := service.UpdateDSTConfig(panel.DSTConfigPatch{Revision: config.Revision, Files: map[string]string{"master-world": unsafe}})
		if !errors.Is(err, panel.ErrInvalid) {
			t.Fatalf("unsafe Lua %q error = %v", unsafe, err)
		}
	}
}

func TestDSTWorldSettingsRejectsUnknownStaleInvalidAndRunning(t *testing.T) {
	service, err := NewService(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	document, err := service.DSTWorldSettings()
	if err != nil {
		t.Fatal(err)
	}
	for _, patch := range []panel.DSTWorldSettingsPatch{
		{Revision: document.Revision, Changes: map[string]any{"master.world.unknown": "default"}},
		{Revision: document.Revision, Changes: map[string]any{"master.world.world_size": "gigantic"}},
		{Revision: "stale", Changes: map[string]any{"master.world.world_size": "small"}},
	} {
		if _, err := service.UpdateDSTWorldSettings(patch); !errors.Is(err, panel.ErrInvalid) {
			t.Fatalf("patch %#v error = %v", patch, err)
		}
	}
	service.masterRunning = true
	if _, err := service.UpdateDSTWorldSettings(panel.DSTWorldSettingsPatch{Revision: document.Revision, Changes: map[string]any{"master.world.world_size": "small"}}); !errors.Is(err, panel.ErrUnsafe) {
		t.Fatalf("running update error = %v", err)
	}
}

func TestSetDSTWorldgenOverrideSupportsCompactStaticTables(t *testing.T) {
	for _, test := range []struct {
		name, source, expected string
	}{
		{name: "existing overrides", source: `return { override_enabled = true, overrides = {} }`, expected: `overrides = { world_size = "huge", }`},
		{name: "missing overrides", source: `return { override_enabled = true }`, expected: `overrides = { world_size = "huge", }`},
	} {
		t.Run(test.name, func(t *testing.T) {
			updated, err := setDSTWorldgenOverride(test.source, "world_size", `"huge"`)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(updated, test.expected) {
				t.Fatalf("updated compact table = %q", updated)
			}
			if _, err := parseDSTWorldgen(updated); err != nil {
				t.Fatalf("updated compact table does not parse: %v", err)
			}
		})
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

func findDSTWorldSetting(t *testing.T, document panel.DSTWorldSettings, key string) panel.Setting {
	t.Helper()
	for _, shard := range document.Shards {
		for _, group := range shard.Groups {
			for _, setting := range group.Settings {
				if setting.Key == key {
					return setting
				}
			}
		}
	}
	t.Fatalf("DST world setting %s not found", key)
	return panel.Setting{}
}
