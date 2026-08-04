package palworld

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"hearth/internal/config"
	"hearth/internal/panel"
)

func TestCalculateCPU(t *testing.T) {
	at := time.Now()
	hostCPU, processCPU := calculateCPU(
		hostSample{Idle100NS: 100, Kernel100NS: 200, User100NS: 100},
		hostSample{Idle100NS: 150, Kernel100NS: 300, User100NS: 200},
		processSample{Running: true, PID: 42, CPU100NS: 100},
		processSample{Running: true, PID: 42, CPU100NS: 100 + uint64(runtime.NumCPU())*250_000},
		at,
		at.Add(time.Second),
	)
	if hostCPU != 75 {
		t.Fatalf("hostCPU = %v", hostCPU)
	}
	if processCPU != 2.5 {
		t.Fatalf("processCPU = %v", processCPU)
	}
}

func TestCalculateCPUResetsWhenProcessChanges(t *testing.T) {
	at := time.Now()
	_, processCPU := calculateCPU(
		hostSample{}, hostSample{},
		processSample{Running: true, PID: 1, CPU100NS: 100},
		processSample{Running: true, PID: 2, CPU100NS: 1000},
		at, at.Add(time.Second),
	)
	if processCPU != 0 {
		t.Fatalf("processCPU = %v", processCPU)
	}
}

func TestActionAllowsUnsafeFallbackOnlyForStopAndRestart(t *testing.T) {
	tests := []struct {
		action string
		want   bool
	}{
		{action: "start", want: false},
		{action: "stop", want: true},
		{action: "restart", want: true},
		{action: "update", want: false},
		{action: "backup", want: false},
	}
	for _, test := range tests {
		if got := actionAllowsUnsafeFallback(test.action); got != test.want {
			t.Errorf("actionAllowsUnsafeFallback(%q) = %v, want %v", test.action, got, test.want)
		}
	}
}

func TestApplyDefaultsSetsRetentionAndSteamTimeout(t *testing.T) {
	gameConfig := config.GameConfig{InstallDir: t.TempDir()}
	applyDefaults(&gameConfig)

	if gameConfig.BackupRetentionDays != 30 {
		t.Fatalf("BackupRetentionDays = %d", gameConfig.BackupRetentionDays)
	}
	if gameConfig.BackupMaxTotalGB != 20 {
		t.Fatalf("BackupMaxTotalGB = %d", gameConfig.BackupMaxTotalGB)
	}
	if gameConfig.SteamCmdNoProgressMinutes != 30 {
		t.Fatalf("SteamCmdNoProgressMinutes = %d", gameConfig.SteamCmdNoProgressMinutes)
	}
}

func TestValidateConfigRejectsRetentionValuesThatOverflowDurations(t *testing.T) {
	directory := t.TempDir()
	gameConfig := config.GameConfig{
		InstallDir:                directory,
		SteamCmd:                  filepath.Join(directory, "steamcmd.exe"),
		SettingsFile:              filepath.Join(directory, "settings.ini"),
		Executable:                filepath.Join(directory, "PalServer.exe"),
		ProcessName:               "PalServer.exe",
		BackupRetentionDays:       maxBackupRetentionDays + 1,
		BackupMaxTotalGB:          20,
		SteamCmdNoProgressMinutes: 30,
	}
	err := validateConfig(gameConfig)
	if !errors.Is(err, panel.ErrInvalid) || !strings.Contains(err.Error(), "backupRetentionDays") {
		t.Fatalf("validateConfig() error = %v", err)
	}
}

func TestRunSteamCMDRetriesOnceAfterCleanSelfUpdateExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses a POSIX shell script")
	}
	directory := t.TempDir()
	counterPath := filepath.Join(directory, "attempts")
	scriptPath := filepath.Join(directory, "steamcmd")
	script := fmt.Sprintf(`#!/bin/sh
count=0
if [ -f %q ]; then count=$(cat %q); fi
count=$((count + 1))
printf "%%s" "$count" > %q
if [ "$count" -eq 1 ]; then
  echo "[----] Update complete, launching..."
  exit 0
fi
echo "Success! App '2394010' fully installed."
`, counterPath, counterPath, counterPath)
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(directory, "steamcmd.log")
	service := &Service{config: config.GameConfig{
		SteamCmd:                  scriptPath,
		InstallDir:                directory,
		SteamCmdNoProgressMinutes: 1,
	}}

	outcome, err := service.runSteamCMD(logPath, func(string, int, string) {})
	if err != nil {
		t.Fatalf("runSteamCMD() error = %v", err)
	}
	if outcome != steamUpdateApplied {
		t.Fatalf("runSteamCMD() outcome = %q; want %q", outcome, steamUpdateApplied)
	}
	attempts, err := os.ReadFile(counterPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(attempts) != "2" {
		t.Fatalf("attempts = %q", attempts)
	}
}

func TestRunSteamCMDRetriesOnceAfterSelfUpdateExitError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses a POSIX shell script")
	}
	directory := t.TempDir()
	counterPath := filepath.Join(directory, "attempts")
	scriptPath := filepath.Join(directory, "steamcmd")
	script := fmt.Sprintf(`#!/bin/sh
count=0
if [ -f %q ]; then count=$(cat %q); fi
count=$((count + 1))
printf "%%s" "$count" > %q
if [ "$count" -eq 1 ]; then
  echo "[----] Update complete, launching Steamcmd..."
  exit 7
fi
echo "Success! App '2394010' fully installed."
`, counterPath, counterPath, counterPath)
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(directory, "steamcmd.log")
	service := &Service{config: config.GameConfig{
		SteamCmd:                  scriptPath,
		InstallDir:                directory,
		SteamCmdNoProgressMinutes: 1,
	}}

	outcome, err := service.runSteamCMD(logPath, func(string, int, string) {})
	if err != nil {
		t.Fatalf("runSteamCMD() error = %v", err)
	}
	if outcome != steamUpdateApplied {
		t.Fatalf("runSteamCMD() outcome = %q; want %q", outcome, steamUpdateApplied)
	}
	attempts, err := os.ReadFile(counterPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(attempts) != "2" {
		t.Fatalf("attempts = %q", attempts)
	}
}

func TestRunSteamCMDAttemptUsesSteamCMDDefaultLibrary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses a POSIX shell script")
	}
	steamCmdRoot := t.TempDir()
	argumentsPath := filepath.Join(steamCmdRoot, "arguments")
	steamCmd := filepath.Join(steamCmdRoot, "steamcmd")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" > %q\n", argumentsPath)
	if err := os.WriteFile(steamCmd, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(steamCmdRoot, "steamcmd.log")
	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{config: config.GameConfig{
		SteamCmd:   steamCmd,
		InstallDir: filepath.Join(steamCmdRoot, "steamapps", "common", "PalServer"),
	}}
	err = service.runSteamCMDAttempt(logFile, logPath, time.Second, func(string, int, string) {})
	if closeErr := logFile.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatalf("runSteamCMDAttempt() error = %v", err)
	}
	arguments, err := os.ReadFile(argumentsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(arguments), "+force_install_dir") {
		t.Fatalf("SteamCMD arguments unexpectedly override the default library: %q", arguments)
	}
}

func TestSteamUpdateResultDistinguishesNoOpUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "steamcmd.log")
	if err := os.WriteFile(path, []byte("Success! App '2394010' already up to date.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outcome, completed := steamUpdateResult(path)
	if !completed || outcome != steamUpdateAlreadyCurrent {
		t.Fatalf("steamUpdateResult() = %q, %v", outcome, completed)
	}
}

func TestRunSteamCMDAttemptStopsAfterNoLogProgress(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses a POSIX shell script")
	}
	directory := t.TempDir()
	scriptPath := filepath.Join(directory, "steamcmd")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nsleep 10\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(directory, "steamcmd.log")
	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	service := &Service{config: config.GameConfig{SteamCmd: scriptPath, InstallDir: directory}}

	started := time.Now()
	err = service.runSteamCMDAttempt(
		logFile,
		logPath,
		150*time.Millisecond,
		func(string, int, string) {},
	)
	if err == nil || !strings.Contains(err.Error(), "no log progress") {
		t.Fatalf("runSteamCMDAttempt() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("no-progress termination took %v", elapsed)
	}
}

func TestUnsafeStopFallsBackToCapturedProcess(t *testing.T) {
	service, platform := newUnavailableRESTService(t)
	startedAt := time.Now()
	activity, err := service.RunAction("palworld", panel.ActionRequest{
		Action:      "stop",
		AllowUnsafe: true,
	})
	if err != nil {
		t.Fatalf("RunAction() error = %v", err)
	}
	if elapsed := time.Since(startedAt); elapsed > 250*time.Millisecond {
		t.Fatalf("RunAction() blocked for %v; task creation must return immediately", elapsed)
	}

	completed := waitForActivity(t, service, activity.ID)
	if completed.Status != "success" || completed.Progress != 100 || completed.Stage != "完成" {
		t.Fatalf("completed activity = %#v", completed)
	}
	if completed.Title != "服务器已强制停止" {
		t.Fatalf("completed title = %q", completed.Title)
	}
	if got := platform.terminatedPIDs(); len(got) != 1 || got[0] != 42 {
		t.Fatalf("terminated PIDs = %v; want [42]", got)
	}
}

func TestConfirmedFallbackStillPrefersRESTSafeStop(t *testing.T) {
	client, err := newRESTClient("http://127.0.0.1:8212", "admin", func() (string, error) {
		return "test-password", nil
	})
	if err != nil {
		t.Fatalf("newRESTClient() error = %v", err)
	}
	platform := &fakePlatform{running: true, pid: 42, startedAt: time.Now().Add(-time.Minute)}
	client.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if strings.HasSuffix(request.URL.Path, "/shutdown") {
			platform.setRunning(false, 0)
		}
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Status:     "204 No Content",
			Header:     make(http.Header),
			Body:       http.NoBody,
		}, nil
	})
	service := &Service{
		config:   configForTest(t),
		platform: platform,
		rest:     client,
	}

	activity, err := service.RunAction("palworld", panel.ActionRequest{
		Action:      "stop",
		AllowUnsafe: true,
	})
	if err != nil {
		t.Fatalf("RunAction() error = %v", err)
	}
	completed := waitForActivity(t, service, activity.ID)
	if completed.Status != "success" || completed.Title != "服务器已安全停止" {
		t.Fatalf("completed activity = %#v", completed)
	}
	if got := platform.terminatedPIDs(); len(got) != 0 {
		t.Fatalf("terminated PIDs = %v; REST safe stop must not force termination", got)
	}
}

func TestShutdownWaitUsesFastDelayWhenNoPlayersAreOnline(t *testing.T) {
	client, err := newRESTClient("http://127.0.0.1:8212", "admin", func() (string, error) {
		return "test-password", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	client.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(request.URL.Path, "/players") {
			t.Fatalf("unexpected request path %s", request.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"players":[]}`)),
		}, nil
	})
	service := &Service{config: config.GameConfig{ShutdownWaitSeconds: 30}, rest: client}
	waitSeconds, detail := service.shutdownWaitSeconds()
	if waitSeconds != emptyServerShutdownWaitSeconds || !strings.Contains(detail, "没有玩家") {
		t.Fatalf("shutdown wait = %d, detail = %q", waitSeconds, detail)
	}
}

func TestShutdownWaitKeepsConfiguredDelayWhenPlayersAreOnline(t *testing.T) {
	client, err := newRESTClient("http://127.0.0.1:8212", "admin", func() (string, error) {
		return "test-password", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	client.client.Transport = roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"players":[{"name":"one"},{"name":"two"}]}`)),
		}, nil
	})
	service := &Service{config: config.GameConfig{ShutdownWaitSeconds: 30}, rest: client}
	waitSeconds, detail := service.shutdownWaitSeconds()
	if waitSeconds != 30 || !strings.Contains(detail, "2 名玩家") {
		t.Fatalf("shutdown wait = %d, detail = %q", waitSeconds, detail)
	}
}

func TestShutdownWaitKeepsConfiguredDelayWhenPlayerListIsMissing(t *testing.T) {
	client, err := newRESTClient("http://127.0.0.1:8212", "admin", func() (string, error) {
		return "test-password", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	client.client.Transport = roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{}`)),
		}, nil
	})
	service := &Service{config: config.GameConfig{ShutdownWaitSeconds: 30}, rest: client}
	waitSeconds, detail := service.shutdownWaitSeconds()
	if waitSeconds != 30 || !strings.Contains(detail, "无法确认") {
		t.Fatalf("shutdown wait = %d, detail = %q", waitSeconds, detail)
	}
}

func TestUnsafeRestartSkipsUnavailableRESTHealthCheck(t *testing.T) {
	service, platform := newUnavailableRESTService(t)
	activity, err := service.RunAction("palworld", panel.ActionRequest{
		Action:      "restart",
		AllowUnsafe: true,
	})
	if err != nil {
		t.Fatalf("RunAction() error = %v", err)
	}

	completed := waitForActivity(t, service, activity.ID)
	if completed.Status != "success" || completed.Title != "服务器已强制重启" {
		t.Fatalf("completed activity = %#v", completed)
	}
	if process, _, sampleErr := platform.sample("PalServer.exe", ""); sampleErr != nil || !process.Running {
		t.Fatalf("process after restart = %#v, error = %v", process, sampleErr)
	}
}

func TestUnsafeConfirmationCannotBypassUpdateSafety(t *testing.T) {
	service, platform := newUnavailableRESTService(t)
	_, err := service.RunAction("palworld", panel.ActionRequest{
		Action:      "update",
		AllowUnsafe: true,
	})
	if !errors.Is(err, panel.ErrInvalid) {
		t.Fatalf("RunAction() error = %v; want ErrInvalid", err)
	}
	if got := platform.terminatedPIDs(); len(got) != 0 {
		t.Fatalf("terminated PIDs = %v; update must never use force stop", got)
	}
}

func TestCachedAPIStatusRefreshesWithoutBlockingOverview(t *testing.T) {
	client, err := newRESTClient("http://127.0.0.1:8212", "admin", func() (string, error) {
		return "test-password", nil
	})
	if err != nil {
		t.Fatalf("newRESTClient() error = %v", err)
	}
	started := make(chan struct{}, 3)
	release := make(chan struct{})
	client.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		started <- struct{}{}
		<-release
		body := `{}`
		if strings.HasSuffix(request.URL.Path, "/players") {
			body = `{"players":[]}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})
	service := &Service{rest: client}

	startedAt := time.Now()
	status := service.cachedAPIStatus()
	if elapsed := time.Since(startedAt); elapsed > 100*time.Millisecond {
		t.Fatalf("cachedAPIStatus() blocked for %v", elapsed)
	}
	if status.InfoOK || status.MetricsOK || status.PlayerListOK {
		t.Fatalf("initial cached status = %#v; want empty status", status)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("background REST refresh did not start")
	}
	close(release)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		service.apiMu.Lock()
		refreshing := service.apiRefreshing
		refreshed := service.apiStatus
		service.apiMu.Unlock()
		if !refreshing {
			if !refreshed.InfoOK || !refreshed.MetricsOK || !refreshed.PlayerListOK {
				t.Fatalf("refreshed status = %#v", refreshed)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("background REST refresh did not finish")
}

func TestForceStopRefusesChangedProcess(t *testing.T) {
	startedAt := time.Now().Add(-time.Minute)
	platform := &fakePlatform{running: true, pid: 99, startedAt: startedAt}
	service := &Service{
		config:   configForTest(t),
		platform: platform,
	}
	err := service.forceStop(
		processSample{Running: true, PID: 42, StartedAt: startedAt},
		func(string, int, string) {},
	)
	if err == nil {
		t.Fatal("forceStop() succeeded after PID changed")
	}
	if got := platform.terminatedPIDs(); len(got) != 0 {
		t.Fatalf("terminated PIDs = %v; changed process must remain untouched", got)
	}
}

func TestForceStopRefusesReusedPID(t *testing.T) {
	startedAt := time.Now().Add(-time.Minute)
	platform := &fakePlatform{
		running: true, pid: 42, startedAt: startedAt.Add(time.Second),
	}
	service := &Service{
		config:   configForTest(t),
		platform: platform,
	}
	err := service.forceStop(
		processSample{Running: true, PID: 42, StartedAt: startedAt},
		func(string, int, string) {},
	)
	if err == nil {
		t.Fatal("forceStop() succeeded after PID was reused by a newer process")
	}
	if got := platform.terminatedPIDs(); len(got) != 0 {
		t.Fatalf("terminated PIDs = %v; reused PID must remain untouched", got)
	}
}

func TestOverviewUsesEmptyActivityArray(t *testing.T) {
	service := Service{}
	activities := service.activitySnapshot()
	if activities == nil {
		t.Fatal("activities must encode as an empty JSON array, not null")
	}
}

func TestApplyAPIStatusPrefersPlayerList(t *testing.T) {
	game := panel.Game{PlayersMax: 8}
	applyAPIStatus(&game, apiStatus{
		Info:         serverInfo{Version: "1.0.2"},
		Metrics:      serverMetrics{CurrentPlayerNum: 4, MaxPlayerNum: 8, Uptime: 120},
		PlayerCount:  2,
		Players:      []panel.OnlinePlayer{{Name: "Moss"}, {Name: "Nia"}},
		InfoOK:       true,
		MetricsOK:    true,
		PlayerListOK: true,
	})

	if game.PlayersOnline != 2 || !game.PlayersAvailable || !game.PlayersMaxKnown {
		t.Fatalf("players = %d available = %v; want player list count", game.PlayersOnline, game.PlayersAvailable)
	}
	if game.PlayersSource != "REST API 玩家列表" || game.Version != "1.0.2" || game.UptimeSeconds != 120 {
		t.Fatalf("game status = %#v", game)
	}
	if len(game.Players) != 2 || game.Players[0].Name != "Moss" || game.Players[1].Name != "Nia" {
		t.Fatalf("player list = %#v", game.Players)
	}
}

func TestApplyAPIStatusFallsBackToMetrics(t *testing.T) {
	game := panel.Game{PlayersMax: 8}
	applyAPIStatus(&game, apiStatus{
		Metrics:   serverMetrics{CurrentPlayerNum: 3, MaxPlayerNum: 6},
		MetricsOK: true,
	})

	if game.PlayersOnline != 3 || game.PlayersMax != 6 || !game.PlayersMaxKnown || !game.PlayersAvailable {
		t.Fatalf("game status = %#v", game)
	}
	if game.PlayersSource != "REST API 指标" {
		t.Fatalf("players source = %q", game.PlayersSource)
	}
}

func TestPublicOnlinePlayersExposeOnlySanitizedDisplayNames(t *testing.T) {
	longName := strings.Repeat("帕", 70)
	players := publicOnlinePlayers([]serverPlayer{
		{Name: "  Moss\n\u202ePlayer  ", Account: "secret-account", PlayerID: "secret-player", UserID: "secret-user"},
		{Name: "\t\r"},
		{Name: longName},
	})
	if len(players) != 3 {
		t.Fatalf("players = %#v", players)
	}
	if players[0].Name != "MossPlayer" || players[1].Name != "未命名玩家" {
		t.Fatalf("sanitized players = %#v", players)
	}
	if len([]rune(players[2].Name)) != 64 {
		t.Fatalf("long player name length = %d", len([]rune(players[2].Name)))
	}
}

func TestSnapshotDoesNotUseINIPlayerLimitWhenWorldOptionExists(t *testing.T) {
	installDir := t.TempDir()
	settingsPath := filepath.Join(installDir, "PalWorldSettings.ini")
	if err := os.WriteFile(settingsPath, []byte("OptionSettings=(ServerPlayerMaxNum=32)\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const worldID = "0123456789ABCDEF0123456789ABCDEF"
	configDirectory := filepath.Join(installDir, "Pal", "Saved", "Config", "WindowsServer")
	worldDirectory := filepath.Join(installDir, "Pal", "Saved", "SaveGames", "0", worldID)
	if err := os.MkdirAll(configDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(worldDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(configDirectory, "GameUserSettings.ini"),
		[]byte("DedicatedServerName="+worldID+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worldDirectory, "Level.sav"), []byte("level"), 0o600); err != nil {
		t.Fatal(err)
	}
	worldOptionPath := filepath.Join(worldDirectory, "WorldOption.sav")
	if err := os.WriteFile(worldOptionPath, []byte("option"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		config: config.GameConfig{
			InstallDir: installDir, SettingsFile: settingsPath, ProcessName: "PalServer.exe",
		},
		platform: &fakePlatform{},
	}
	game, _ := service.snapshot()
	if game.PlayersMaxKnown || game.PlayersMax != 0 {
		t.Fatalf("WorldOption fallback = %d known=%v; want unknown", game.PlayersMax, game.PlayersMaxKnown)
	}

	if err := os.Remove(worldOptionPath); err != nil {
		t.Fatal(err)
	}
	game, _ = service.snapshot()
	if !game.PlayersMaxKnown || game.PlayersMax != 32 {
		t.Fatalf("INI fallback = %d known=%v; want 32 known", game.PlayersMax, game.PlayersMaxKnown)
	}
}

type fakePlatform struct {
	mu         sync.Mutex
	running    bool
	pid        uint32
	startedAt  time.Time
	terminated []uint32
}

func (p *fakePlatform) sample(processName, _ string) (processSample, hostSample, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if processName == "steamcmd.exe" {
		return processSample{}, hostSample{}, nil
	}
	return processSample{Running: p.running, PID: p.pid, StartedAt: p.startedAt}, hostSample{}, nil
}

func (p *fakePlatform) startDetached(_, _ string, _ []string, _ string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.running = true
	p.pid = 84
	p.startedAt = time.Now()
	return nil
}

func (p *fakePlatform) terminate(processID uint32, startedAt time.Time) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.running || p.pid != processID || !p.startedAt.Equal(startedAt) {
		return errors.New("process identity changed")
	}
	p.terminated = append(p.terminated, processID)
	p.running = false
	p.startedAt = time.Time{}
	return nil
}

func (p *fakePlatform) terminatedPIDs() []uint32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]uint32(nil), p.terminated...)
}

func (p *fakePlatform) setRunning(running bool, pid uint32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.running = running
	p.pid = pid
	if !running {
		p.startedAt = time.Time{}
	}
}

func newUnavailableRESTService(t *testing.T) (*Service, *fakePlatform) {
	t.Helper()
	client, err := newRESTClient("http://127.0.0.1:8212", "admin", func() (string, error) {
		return "test-password", nil
	})
	if err != nil {
		t.Fatalf("newRESTClient() error = %v", err)
	}
	client.client.Transport = roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Status:     "503 Service Unavailable",
			Header:     make(http.Header),
			Body:       http.NoBody,
		}, nil
	})
	platform := &fakePlatform{running: true, pid: 42, startedAt: time.Now().Add(-time.Minute)}
	return &Service{
		config:   configForTest(t),
		platform: platform,
		rest:     client,
	}, platform
}

func configForTest(t *testing.T) config.GameConfig {
	t.Helper()
	return config.GameConfig{
		InstallDir:          t.TempDir(),
		SteamCmd:            "steamcmd.exe",
		Executable:          "PalServer.exe",
		ProcessName:         "PalServer.exe",
		ShutdownWaitSeconds: 1,
	}
}

func waitForActivity(t *testing.T, service *Service, id string) panel.Activity {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, activity := range service.activitySnapshot() {
			if activity.ID == id && activity.Status != "running" {
				return activity
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("activity %s did not finish", id)
	return panel.Activity{}
}
