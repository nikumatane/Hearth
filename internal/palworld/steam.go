package palworld

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"hearth/internal/panel"
	"hearth/internal/steamapp"
)

const (
	versionCheckUnchecked   = steamapp.VersionCheckUnchecked
	versionCheckChecking    = steamapp.VersionCheckChecking
	versionCheckCurrent     = steamapp.VersionCheckCurrent
	versionCheckAvailable   = steamapp.VersionCheckAvailable
	versionCheckUnavailable = steamapp.VersionCheckUnavailable
)

type steamVersionStatus = steamapp.VersionStatus

func (s *Service) checkVersion(report taskReporter, registerLog taskLogReporter) error {
	installedBuildID := s.installedBuildID()
	s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckChecking})
	if _, err := strconv.ParseUint(installedBuildID, 10, 32); err != nil {
		s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
		return fmt.Errorf("read installed Steam build ID: %w", err)
	}
	installedSnapshot, err := steamapp.ReadInstalled(s.appManifestPath())
	if err != nil {
		s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
		return fmt.Errorf("read installed Steam depot manifests: %w", err)
	}

	logDirectory := filepath.Join(s.config.InstallDir, "panel-logs")
	if err := os.MkdirAll(logDirectory, 0o700); err != nil {
		s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
		return err
	}
	logName := taskLogName("steamcmd-version-check")
	logPath := filepath.Join(logDirectory, logName)
	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|os.O_APPEND, 0o600)
	if err != nil {
		s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
		return err
	}
	if registerLog != nil {
		registerLog(logName, "SteamCMD 版本检查日志")
	}

	report("准备 SteamCMD", 10, "正在完成 SteamCMD 自身检查；其版本不参与 Palworld 更新判断")
	if err := s.runSteamVersionCommand(logFile, s.steamNoProgressTimeout(), "+quit"); err != nil {
		_ = logFile.Close()
		s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
		return fmt.Errorf("prepare SteamCMD for version check: %w", err)
	}
	report("准备 SteamCMD", 35, "SteamCMD 已准备完成，开始独立查询 Palworld Dedicated Server")
	if _, err := fmt.Fprintln(logFile, "\n[Hearth] Querying Palworld Dedicated Server app 2394010 after SteamCMD preparation."); err != nil {
		_ = logFile.Close()
		s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
		return err
	}
	report("查询 Palworld", 50, "正在刷新 App 2394010 的 public 分支信息；不会修改服务端文件")
	if err := s.runSteamVersionCommand(
		logFile,
		2*time.Minute,
		"+login", "anonymous",
		"+app_info_update", "1",
		"+app_info_print", palworldAppID,
		"+quit",
	); err != nil {
		_ = logFile.Close()
		s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
		return fmt.Errorf("query Palworld server version: %w", err)
	}
	if err := logFile.Close(); err != nil {
		s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
		return err
	}

	report("解析服务端版本", 85, "正在比较本机服务端 manifest 与 Palworld public 分支")
	availableSnapshot, err := steamapp.ReadPublicLog(logPath, palworldAppID)
	if err != nil {
		s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
		return err
	}
	status, err := steamapp.Compare(installedSnapshot, availableSnapshot)
	if err != nil {
		s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
		return err
	}
	s.setVersionStatus(installedBuildID, status)
	if status.UpdateAvailable {
		report("检查完成", 100, "Palworld Dedicated Server 的 public 分支有可用更新")
	} else {
		report("检查完成", 100, "Palworld Dedicated Server 当前已是 public 分支最新版")
	}
	return nil
}

func depotManifestsFromAppManifest(path string) (map[string]string, error) {
	snapshot, err := steamapp.ReadInstalled(path)
	return snapshot.Depots, err
}

func publicDepotManifestsFromLog(path, appID string) (map[string]string, error) {
	snapshot, err := steamapp.ReadPublicLog(path, appID)
	return snapshot.Depots, err
}

func parseInstalledDepotManifests(scanner *bufio.Scanner) (map[string]string, error) {
	snapshot, err := steamapp.ParseInstalled(scanner)
	return snapshot.Depots, err
}

func parsePublicDepotManifests(scanner *bufio.Scanner, appID string) (map[string]string, error) {
	snapshot, err := steamapp.ParsePublic(scanner, appID)
	return snapshot.Depots, err
}

func compareSteamDepotManifests(installed, available map[string]string) (steamVersionStatus, error) {
	return steamapp.Compare(
		steamapp.ManifestSnapshot{Depots: installed},
		steamapp.ManifestSnapshot{Depots: available},
	)
}

func (s *Service) runSteamVersionCommand(logFile *os.File, noProgressTimeout time.Duration, arguments ...string) error {
	logPath := logFile.Name()
	initialSize := logFileSize(logPath)
	command := exec.Command(s.config.SteamCmd, arguments...)
	command.Dir = filepath.Dir(s.config.SteamCmd)
	command.Stdout, command.Stderr = logFile, logFile
	prepareManagedCommand(command)
	if err := command.Start(); err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	pollInterval := 2 * time.Second
	if noProgressTimeout < 8*time.Second {
		pollInterval = max(50*time.Millisecond, noProgressTimeout/4)
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	lastSize := initialSize
	lastProgressAt := time.Now()
	for {
		select {
		case err := <-done:
			return err
		case <-ticker.C:
			size := logFileSize(logPath)
			if size > lastSize {
				lastSize = size
				lastProgressAt = time.Now()
			}
			if time.Since(lastProgressAt) < noProgressTimeout {
				continue
			}
			terminateErr := terminateManagedCommand(command)
			waitErr := <-done
			if terminateErr != nil {
				return fmt.Errorf("produced no log progress for %s; terminate process tree: %w", noProgressTimeout, terminateErr)
			}
			if waitErr != nil {
				return fmt.Errorf("produced no log progress for %s and was terminated: %w", noProgressTimeout, waitErr)
			}
			return fmt.Errorf("produced no log progress for %s and was terminated", noProgressTimeout)
		}
	}
}

func (s *Service) runAutomaticVersionChecks() {
	timer := time.NewTimer(automaticVersionCheckInitialDelay)
	defer timer.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-timer.C:
		}
		if s.versionCheckDue() {
			if _, err := s.RunAction(palworldID, panel.ActionRequest{Action: "check-update"}); err != nil &&
				!errors.Is(err, panel.ErrBusy) && !errors.Is(err, panel.ErrUnsafe) {
				slog.Warn("automatic Palworld version check was skipped", "error", err)
			}
		}
		timer.Reset(automaticVersionCheckPollInterval)
	}
}

func (s *Service) versionCheckDue() bool {
	s.versionMu.Lock()
	defer s.versionMu.Unlock()
	if s.versionStatus.State == versionCheckChecking {
		return false
	}
	if !s.versionCheckedAt.IsZero() {
		return time.Since(s.versionCheckedAt) >= automaticVersionCheckInterval
	}
	return s.versionAttemptedAt.IsZero() ||
		time.Since(s.versionAttemptedAt) >= automaticVersionCheckFailureRetry
}
