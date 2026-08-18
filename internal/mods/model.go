// Package mods defines the cross-game safety contract for future mod adapters.
// It intentionally has no HTTP or filesystem executor in 1.4.0: unfinished
// Palworld and DST operations remain invisible and unreachable from the panel.
package mods

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type Source string

const (
	SourceOfficialPackage Source = "official_package"
	SourceSteamWorkshop   Source = "steam_workshop"
	SourceExisting        Source = "existing"
)

type Ownership string

const (
	OwnershipHearth   Ownership = "hearth"
	OwnershipExternal Ownership = "external"
)

type Descriptor struct {
	ID              string
	GameID          string
	Name            string
	Source          Source
	SourceReference string
	Version         string
	Enabled         bool
	Ownership       Ownership
	Dependencies    []string
}

type Inventory struct {
	GameID    string
	Revision  string
	Managed   bool
	Mods      []Descriptor
	ScannedAt time.Time
}

type Action string

const (
	ActionInstall Action = "install"
	ActionUpdate  Action = "update"
	ActionEnable  Action = "enable"
	ActionDisable Action = "disable"
	ActionRemove  Action = "remove"
)

type ChangeRequest struct {
	Action Action
	Mod    Descriptor
}

type Change struct {
	Action          Action
	ModID           string
	FromVersion     string
	ToVersion       string
	RequiresStopped bool
	RequiresBackup  bool
	Warnings        []string
}

type Plan struct {
	GameID            string
	InventoryRevision string
	Changes           []Change
	CreatedAt         time.Time
}

type ExecutionRecord struct {
	Plan              Plan
	SnapshotReference string
	StartedAt         time.Time
	CompletedAt       time.Time
	RolledBack        bool
	Error             string
}

// BuildPlan performs only deterministic validation. Adapters must separately
// acquire packages, verify their game-specific metadata and execute the plan.
func BuildPlan(inventory Inventory, requests []ChangeRequest, now time.Time) (Plan, error) {
	if strings.TrimSpace(inventory.GameID) == "" || strings.TrimSpace(inventory.Revision) == "" {
		return Plan{}, errors.New("mod inventory identity is incomplete")
	}
	if !inventory.Managed {
		return Plan{}, errors.New("mod changes require a managed game")
	}
	existing := make(map[string]Descriptor, len(inventory.Mods))
	for _, mod := range inventory.Mods {
		if err := validateDescriptor(mod, inventory.GameID); err != nil {
			return Plan{}, err
		}
		if _, duplicate := existing[mod.ID]; duplicate {
			return Plan{}, fmt.Errorf("duplicate mod id %q", mod.ID)
		}
		existing[mod.ID] = mod
	}
	plan := Plan{GameID: inventory.GameID, InventoryRevision: inventory.Revision, CreatedAt: now}
	seen := make(map[string]struct{}, len(requests))
	for _, request := range requests {
		key := string(request.Action) + "\x00" + request.Mod.ID
		if _, duplicate := seen[key]; duplicate {
			return Plan{}, fmt.Errorf("duplicate mod change %q", request.Mod.ID)
		}
		seen[key] = struct{}{}
		current, installed := existing[request.Mod.ID]
		change := Change{
			Action: request.Action, ModID: request.Mod.ID,
			RequiresStopped: true, RequiresBackup: true,
		}
		switch request.Action {
		case ActionInstall:
			if installed {
				return Plan{}, fmt.Errorf("mod %q is already installed", request.Mod.ID)
			}
			if err := validateDescriptor(request.Mod, inventory.GameID); err != nil {
				return Plan{}, err
			}
			if request.Mod.Ownership != OwnershipHearth {
				return Plan{}, errors.New("new mod installations must be owned by Hearth")
			}
			change.ToVersion = request.Mod.Version
			if len(request.Mod.Dependencies) > 0 {
				change.Warnings = append(change.Warnings, "dependencies require adapter verification")
			}
		case ActionUpdate:
			if !installed {
				return Plan{}, fmt.Errorf("mod %q is not installed", request.Mod.ID)
			}
			if current.Ownership != OwnershipHearth {
				return Plan{}, fmt.Errorf("mod %q is externally owned and cannot be updated", request.Mod.ID)
			}
			change.FromVersion, change.ToVersion = current.Version, request.Mod.Version
			if strings.TrimSpace(change.ToVersion) == "" || change.ToVersion == change.FromVersion {
				return Plan{}, fmt.Errorf("mod %q update version is unchanged or empty", request.Mod.ID)
			}
		case ActionEnable, ActionDisable:
			if !installed {
				return Plan{}, fmt.Errorf("mod %q is not installed", request.Mod.ID)
			}
			change.FromVersion, change.ToVersion = current.Version, current.Version
		case ActionRemove:
			if !installed {
				return Plan{}, fmt.Errorf("mod %q is not installed", request.Mod.ID)
			}
			if current.Ownership != OwnershipHearth {
				return Plan{}, fmt.Errorf("mod %q is externally owned and cannot be removed", request.Mod.ID)
			}
			change.FromVersion = current.Version
		default:
			return Plan{}, fmt.Errorf("unsupported mod action %q", request.Action)
		}
		plan.Changes = append(plan.Changes, change)
	}
	return plan, nil
}

func validateDescriptor(mod Descriptor, gameID string) error {
	if strings.TrimSpace(mod.ID) == "" || strings.ContainsAny(mod.ID, "\x00\r\n/\\") {
		return errors.New("mod id is empty or unsafe")
	}
	if mod.GameID != gameID {
		return fmt.Errorf("mod %q belongs to another game", mod.ID)
	}
	if strings.TrimSpace(mod.Name) == "" || strings.TrimSpace(mod.SourceReference) == "" {
		return fmt.Errorf("mod %q metadata is incomplete", mod.ID)
	}
	switch mod.Source {
	case SourceOfficialPackage, SourceSteamWorkshop, SourceExisting:
	default:
		return fmt.Errorf("mod %q source is unsupported", mod.ID)
	}
	switch mod.Ownership {
	case OwnershipHearth, OwnershipExternal:
	default:
		return fmt.Errorf("mod %q ownership is unsupported", mod.ID)
	}
	return nil
}
