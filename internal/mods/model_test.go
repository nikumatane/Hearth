package mods

import (
	"strings"
	"testing"
	"time"
)

func TestBuildPlanRejectsRemovalOfExternallyOwnedMod(t *testing.T) {
	inventory := Inventory{
		GameID: "palworld", Revision: "revision-1", Managed: true,
		Mods: []Descriptor{{
			ID: "existing-mod", GameID: "palworld", Name: "Existing", Source: SourceExisting,
			SourceReference: "Mods/existing-mod", Version: "1", Ownership: OwnershipExternal,
		}},
	}
	_, err := BuildPlan(inventory, []ChangeRequest{{Action: ActionRemove, Mod: Descriptor{ID: "existing-mod"}}}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "externally owned") {
		t.Fatalf("BuildPlan() error = %v", err)
	}
}

func TestBuildPlanRequiresStopAndBackupForManagedChanges(t *testing.T) {
	inventory := Inventory{
		GameID: "dont-starve-together", Revision: "revision-1", Managed: true,
		Mods: []Descriptor{{
			ID: "workshop-1", GameID: "dont-starve-together", Name: "Workshop Mod",
			Source: SourceSteamWorkshop, SourceReference: "workshop:1", Version: "1", Ownership: OwnershipHearth,
		}},
	}
	plan, err := BuildPlan(inventory, []ChangeRequest{{
		Action: ActionUpdate,
		Mod:    Descriptor{ID: "workshop-1", GameID: "dont-starve-together", Version: "2"},
	}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) != 1 || !plan.Changes[0].RequiresStopped || !plan.Changes[0].RequiresBackup {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestBuildPlanRejectsUnmanagedGame(t *testing.T) {
	_, err := BuildPlan(Inventory{GameID: "palworld", Revision: "revision-1"}, nil, time.Now())
	if err == nil || !strings.Contains(err.Error(), "managed game") {
		t.Fatalf("BuildPlan() error = %v", err)
	}
}
