package dst

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"hearth/internal/panel"
	"hearth/internal/steamapp"
)

const (
	dstAppID                        = "343050"
	versionCheckUnchecked           = steamapp.VersionCheckUnchecked
	versionCheckChecking            = steamapp.VersionCheckChecking
	versionCheckCurrent             = steamapp.VersionCheckCurrent
	versionCheckAvailable           = steamapp.VersionCheckAvailable
	versionCheckUnavailable         = steamapp.VersionCheckUnavailable
	automaticVersionCheckInterval   = 6 * time.Hour
	automaticVersionCheckRetry      = time.Hour
	automaticVersionCheckPoll       = 15 * time.Minute
	automaticVersionCheckInitial    = 45 * time.Second
	defaultNoProgressTimeoutMinutes = 30
)

type steamVersionStatus = steamapp.VersionStatus
type versionReporter func(stage string, progress int, detail string)

func (s *Service) validateSteamVersionCheck() error {
	steamCmd := strings.TrimSpace(s.config.SteamCmd)
	if steamCmd == "" || !filepath.IsAbs(steamCmd) {
		return fmt.Errorf("%w: DST 尚未配置有效的 SteamCMD，无法检查服务端版本", panel.ErrInvalid)
	}
	info, err := os.Stat(steamCmd)
	if err != nil || info.IsDir() {
		return fmt.Errorf("%w: DST SteamCMD 不可用: %s", panel.ErrInvalid, steamCmd)
	}
	return nil
}

func (s *Service) checkVersion(report versionReporter, logID string) error {
	installedBuildID := s.installedBuildID()
	s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckChecking})
	if _, err := strconv.ParseUint(installedBuildID, 10, 32); err != nil {
		s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
		return fmt.Errorf("read installed DST Steam build ID: %w", err)
	}
	installed, err := steamapp.ReadInstalled(s.appManifestPath())
	if err != nil {
		s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
		return fmt.Errorf("read installed DST depot manifests: %w", err)
	}

	logPath, ok := s.TaskLogPath(logID)
	if !ok {
		s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
		return errors.New("DST version-check log path is unavailable")
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
		return err
	}
	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|os.O_APPEND, 0o600)
	if err != nil {
		s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
		return err
	}

	report("准备 SteamCMD", 10, "正在完成 SteamCMD 自身检查；其版本不参与 DST 更新判断")
	if err := s.runSteamVersionCommand(logFile, s.steamNoProgressTimeout(), "+quit"); err != nil {
		_ = logFile.Close()
		s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
		return fmt.Errorf("prepare SteamCMD for DST version check: %w", err)
	}
	report("查询 DST", 45, "正在刷新 App 343050 的 public 分支；不会修改 Dedicated Server 文件")
	_, _ = fmt.Fprintln(logFile, "\n[Hearth] Querying Don't Starve Together Dedicated Server app 343050.")
	if err := s.runSteamVersionCommand(
		logFile, 2*time.Minute,
		"+login", "anonymous", "+app_info_update", "1", "+app_info_print", dstAppID, "+quit",
	); err != nil {
		_ = logFile.Close()
		s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
		return fmt.Errorf("query DST server version: %w", err)
	}
	if err := logFile.Close(); err != nil {
		s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
		return err
	}

	report("比较 depot manifest", 85, "正在对比本机 DST 与 public 分支的 depot manifest")
	available, err := steamapp.ReadPublicLog(logPath, dstAppID)
	if err != nil {
		s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
		return fmt.Errorf("parse DST SteamCMD app info: %w", err)
	}
	status, err := steamapp.Compare(installed, available)
	if err != nil {
		s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
		return err
	}
	s.setVersionStatus(installedBuildID, status)
	if status.UpdateAvailable {
		report("检查完成", 100, "DST Dedicated Server 的 public 分支有可用更新；本版本仅提示，不执行更新")
	} else {
		report("检查完成", 100, "DST Dedicated Server 当前已是 public 分支最新版")
	}
	return nil
}

func (s *Service) runSteamVersionCommand(logFile *os.File, noProgressTimeout time.Duration, arguments ...string) error {
	logPath := logFile.Name()
	initialSize := fileSize(logPath)
	command := exec.Command(s.config.SteamCmd, arguments...)
	command.Dir = filepath.Dir(s.config.SteamCmd)
	command.Stdout, command.Stderr = logFile, logFile
	prepareCommand(command)
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
	lastSize, lastProgressAt := initialSize, time.Now()
	for {
		select {
		case err := <-done:
			return err
		case <-ticker.C:
			size := fileSize(logPath)
			if size > lastSize {
				lastSize, lastProgressAt = size, time.Now()
			}
			if time.Since(lastProgressAt) < noProgressTimeout {
				continue
			}
			terminateErr := terminateCommand(command)
			waitErr := <-done
			if terminateErr != nil {
				return fmt.Errorf("SteamCMD produced no log progress for %s; terminate process tree: %w", noProgressTimeout, terminateErr)
			}
			if waitErr != nil {
				return fmt.Errorf("SteamCMD produced no log progress for %s and was terminated: %w", noProgressTimeout, waitErr)
			}
			return fmt.Errorf("SteamCMD produced no log progress for %s and was terminated", noProgressTimeout)
		}
	}
}

func (s *Service) steamNoProgressTimeout() time.Duration {
	minutes := s.config.SteamCmdNoProgressMinutes
	if minutes <= 0 {
		minutes = defaultNoProgressTimeoutMinutes
	}
	return time.Duration(minutes) * time.Minute
}

func (s *Service) appManifestPath() string {
	return filepath.Join(filepath.Dir(s.config.SteamCmd), "steamapps", "appmanifest_"+dstAppID+".acf")
}

func (s *Service) installedBuildID() string {
	if strings.TrimSpace(s.config.SteamCmd) == "" {
		return "未知"
	}
	return steamapp.ReadBuildID(s.appManifestPath())
}

func applyVersionStatus(game *panel.Game, status steamVersionStatus) {
	game.VersionCheck = status.State
	game.UpdateAvailable = status.UpdateAvailable
	game.AvailableVersion = status.AvailableVersion
}

func (s *Service) versionStatusForBuild(buildID string) steamVersionStatus {
	if buildID == "" || buildID == "未知" {
		return steamVersionStatus{State: versionCheckUnavailable}
	}
	s.versionMu.Lock()
	defer s.versionMu.Unlock()
	if buildID != s.versionBuildID {
		s.versionBuildID = buildID
		s.versionStatus = steamVersionStatus{State: versionCheckUnchecked}
		s.versionCheckedAt = time.Time{}
		s.versionAttemptedAt = time.Time{}
	}
	return s.versionStatus
}

func (s *Service) setVersionStatus(buildID string, status steamVersionStatus) {
	s.versionMu.Lock()
	defer s.versionMu.Unlock()
	s.versionBuildID, s.versionStatus = buildID, status
	if status.State == versionCheckChecking {
		s.versionAttemptedAt, s.versionCheckedAt = time.Now(), time.Time{}
	}
	if status.State == versionCheckCurrent || status.State == versionCheckAvailable {
		s.versionCheckedAt = time.Now()
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
	return s.versionAttemptedAt.IsZero() || time.Since(s.versionAttemptedAt) >= automaticVersionCheckRetry
}

func (s *Service) runAutomaticVersionChecks() {
	timer := time.NewTimer(automaticVersionCheckInitial)
	defer timer.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-timer.C:
		}
		if s.versionCheckDue() {
			if _, err := s.RunAction(gameID, panel.ActionRequest{Action: "check-update"}); err != nil &&
				!errors.Is(err, panel.ErrBusy) && !errors.Is(err, panel.ErrUnsafe) {
				s.setVersionStatus(s.installedBuildID(), steamVersionStatus{State: versionCheckUnavailable})
				slog.Warn("automatic DST version check was skipped", "error", err)
			}
		}
		timer.Reset(automaticVersionCheckPoll)
	}
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}
