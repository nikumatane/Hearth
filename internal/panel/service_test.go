package panel

import (
	"errors"
	"testing"
)

func TestDemoServiceOverview(t *testing.T) {
	service := NewDemoService()
	overview := service.Overview()

	if len(overview.Games) != 2 {
		t.Fatalf("len(Games) = %d", len(overview.Games))
	}
	if overview.Host.MemoryTotalGB != 8 {
		t.Fatalf("MemoryTotalGB = %v", overview.Host.MemoryTotalGB)
	}
}

func TestDemoServiceRejectsUnknownAction(t *testing.T) {
	service := NewDemoService()
	_, err := service.RunAction("palworld", ActionRequest{Action: "delete"})
	if !errors.Is(err, ErrBadAction) {
		t.Fatalf("RunAction() error = %v", err)
	}
}

func TestDemoServiceSerializesActionsPerGame(t *testing.T) {
	service := NewDemoService()
	if _, err := service.RunAction("palworld", ActionRequest{Action: "backup"}); err != nil {
		t.Fatalf("first RunAction() error = %v", err)
	}
	if _, err := service.RunAction("palworld", ActionRequest{Action: "restart"}); !errors.Is(err, ErrBusy) {
		t.Fatalf("second RunAction() error = %v", err)
	}
	if _, err := service.RunAction("dont-starve-together", ActionRequest{Action: "start"}); err != nil {
		t.Fatalf("other game RunAction() error = %v", err)
	}
}
