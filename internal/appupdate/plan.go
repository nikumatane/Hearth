package appupdate

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Plan struct {
	Version           string `json:"version"`
	PreviousVersion   string `json:"previousVersion"`
	ParentPID         int    `json:"parentPid"`
	TaskName          string `json:"taskName"`
	StageDir          string `json:"stageDir"`
	InstallDir        string `json:"installDir"`
	TargetExecutable  string `json:"targetExecutable"`
	HealthURL         string `json:"healthUrl"`
	ResultFile        string `json:"resultFile"`
	LogFile           string `json:"logFile"`
	ActorCredentialID string `json:"actorCredentialId"`
	ActorRole         string `json:"actorRole"`
	ActorIP           string `json:"actorIp"`
}

type Result struct {
	State             string    `json:"state"`
	Stage             string    `json:"stage"`
	Version           string    `json:"version"`
	PreviousVersion   string    `json:"previousVersion"`
	Message           string    `json:"message"`
	CompletedAt       time.Time `json:"completedAt"`
	ActorCredentialID string    `json:"actorCredentialId"`
	ActorRole         string    `json:"actorRole"`
	ActorIP           string    `json:"actorIp"`
	Consumed          bool      `json:"consumed,omitempty"`
}

func ReadPlan(path string) (Plan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Plan{}, err
	}
	if len(data) > 64<<10 {
		return Plan{}, errors.New("update plan is too large")
	}
	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return Plan{}, err
	}
	if err := plan.Validate(path); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func (p Plan) Validate(planPath string) error {
	version, ok := parseVersion(p.Version)
	if !ok || version != p.Version {
		return errors.New("invalid update version")
	}
	if strings.TrimSpace(p.PreviousVersion) == "" || len(p.PreviousVersion) > 64 {
		return errors.New("invalid previous version")
	}
	if p.TaskName != updateTaskName || p.ParentPID <= 0 {
		return errors.New("invalid update task or parent process")
	}
	if p.ActorCredentialID == "" || len(p.ActorCredentialID) > 128 || p.ActorRole != "admin" {
		return errors.New("invalid update actor")
	}
	if _, err := netip.ParseAddr(p.ActorIP); err != nil {
		return errors.New("invalid update actor IP")
	}
	stage, err := filepath.Abs(p.StageDir)
	if err != nil {
		return err
	}
	if filepath.Clean(filepath.Dir(planPath)) != filepath.Clean(stage) {
		return errors.New("update plan is outside its staging directory")
	}
	install, err := filepath.Abs(p.InstallDir)
	if err != nil {
		return err
	}
	if filepath.Clean(filepath.Dir(p.TargetExecutable)) != filepath.Clean(install) || !strings.EqualFold(filepath.Base(p.TargetExecutable), "hearth.exe") {
		return errors.New("invalid Hearth target executable")
	}
	if filepath.Clean(stage) == filepath.Clean(install) {
		return errors.New("staging and installation directories must differ")
	}
	stateDirectory := filepath.Dir(stage)
	if !filepath.IsAbs(p.ResultFile) || !filepath.IsAbs(p.LogFile) || filepath.Clean(filepath.Dir(p.ResultFile)) != filepath.Clean(stateDirectory) || filepath.Base(p.ResultFile) != updateResultName ||
		filepath.Clean(filepath.Dir(p.LogFile)) != filepath.Clean(stateDirectory) || filepath.Base(p.LogFile) != updateLogName {
		return errors.New("invalid update result or log path")
	}
	if err := validateHealthURL(p.HealthURL); err != nil {
		return fmt.Errorf("invalid health URL: %w", err)
	}
	for _, name := range []string{"hearth.exe", "hearth-updater.exe", "VERSION"} {
		if info, err := os.Stat(filepath.Join(stage, name)); err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("missing staged %s", name)
		}
	}
	return nil
}

func (s *Service) readResult() (*Result, error) {
	if s.config.Update.StagingDir == "" {
		return nil, nil
	}
	path := filepath.Join(s.config.Update.StagingDir, updateResultName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) > 64<<10 {
		return nil, errors.New("update result is too large")
	}
	var result Result
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	if result.State != "succeeded" && result.State != "rolled_back" && result.State != "failed" {
		return nil, errors.New("update result has an invalid state")
	}
	if _, ok := parseVersion(result.Version); !ok || result.ActorCredentialID == "" || result.ActorRole != "admin" {
		return nil, errors.New("update result has invalid release or actor metadata")
	}
	if _, err := netip.ParseAddr(result.ActorIP); err != nil {
		return nil, errors.New("update result has an invalid actor IP")
	}
	return &result, nil
}
