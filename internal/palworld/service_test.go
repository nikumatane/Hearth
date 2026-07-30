package palworld

import (
	"runtime"
	"testing"
	"time"

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

func TestActionRequiresRESTOnlyForRunningSafetyActions(t *testing.T) {
	tests := []struct {
		action  string
		running bool
		want    bool
	}{
		{action: "start", running: false, want: false},
		{action: "start", running: true, want: false},
		{action: "stop", running: true, want: true},
		{action: "restart", running: true, want: true},
		{action: "update", running: true, want: true},
		{action: "backup", running: true, want: true},
		{action: "update", running: false, want: false},
		{action: "backup", running: false, want: false},
	}
	for _, test := range tests {
		if got := actionRequiresREST(test.action, test.running); got != test.want {
			t.Errorf("actionRequiresREST(%q, %v) = %v, want %v", test.action, test.running, got, test.want)
		}
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
