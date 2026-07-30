package palworld

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
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

func TestValidateManagementSettingsBeforeFirstStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "PalWorldSettings.ini")
	service := Service{config: config.GameConfig{
		SettingsFile: path,
		RESTURL:      "http://127.0.0.1:8212",
	}}

	if err := os.WriteFile(path, []byte(`OptionSettings=(AdminPassword="secret",RESTAPIEnabled=False,RESTAPIPort=8212)`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := service.validateManagementSettings(); !errors.Is(err, panel.ErrUnsafe) {
		t.Fatalf("disabled REST validation error = %v", err)
	}

	if err := os.WriteFile(path, []byte(`OptionSettings=(AdminPassword="secret",RESTAPIEnabled=True,RESTAPIPort=8212)`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := service.validateManagementSettings(); err != nil {
		t.Fatalf("valid management settings error = %v", err)
	}

	if err := os.WriteFile(path, []byte(`OptionSettings=(AdminPassword="secret",RESTAPIEnabled=True,RESTAPIPort=9000)`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := service.validateManagementSettings(); !errors.Is(err, panel.ErrUnsafe) {
		t.Fatalf("mismatched REST port validation error = %v", err)
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
		InfoOK:       true,
		MetricsOK:    true,
		PlayerListOK: true,
	})

	if game.PlayersOnline != 2 || !game.PlayersAvailable {
		t.Fatalf("players = %d available = %v; want player list count", game.PlayersOnline, game.PlayersAvailable)
	}
	if game.PlayersSource != "REST API 玩家列表" || game.Version != "1.0.2" || game.UptimeSeconds != 120 {
		t.Fatalf("game status = %#v", game)
	}
}

func TestApplyAPIStatusFallsBackToMetrics(t *testing.T) {
	game := panel.Game{PlayersMax: 8}
	applyAPIStatus(&game, apiStatus{
		Metrics:   serverMetrics{CurrentPlayerNum: 3, MaxPlayerNum: 6},
		MetricsOK: true,
	})

	if game.PlayersOnline != 3 || game.PlayersMax != 6 || !game.PlayersAvailable {
		t.Fatalf("game status = %#v", game)
	}
	if game.PlayersSource != "REST API 指标" {
		t.Fatalf("players source = %q", game.PlayersSource)
	}
}
