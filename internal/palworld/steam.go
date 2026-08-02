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
	"strings"
	"time"

	"hearth/internal/panel"
)

const (
	versionCheckUnchecked   = "unchecked"
	versionCheckChecking    = "checking"
	versionCheckCurrent     = "current"
	versionCheckAvailable   = "update_available"
	versionCheckUnavailable = "unavailable"
)

type steamVersionStatus struct {
	State            string
	AvailableVersion string
	UpdateAvailable  bool
}

func (s *Service) checkVersion(report taskReporter) error {
	installedBuildID := s.installedBuildID()
	s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckChecking})
	if _, err := strconv.ParseUint(installedBuildID, 10, 32); err != nil {
		s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
		return fmt.Errorf("read installed Steam build ID: %w", err)
	}

	logDirectory := filepath.Join(s.config.InstallDir, "panel-logs")
	if err := os.MkdirAll(logDirectory, 0o700); err != nil {
		s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
		return err
	}
	logPath := filepath.Join(logDirectory, "steamcmd-version-check.log")
	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|os.O_APPEND, 0o600)
	if err != nil {
		s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
		return err
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
	availableBuildID, err := publicBuildIDFromLog(logPath, palworldAppID)
	if err != nil {
		s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
		return err
	}
	status, err := compareSteamBuilds(installedBuildID, availableBuildID)
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
		<-timer.C
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

func publicBuildIDFromLog(path, appID string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	buildID, err := parsePublicBuildID(bufio.NewScanner(file), appID)
	if err != nil {
		return "", fmt.Errorf("parse SteamCMD public build ID: %w", err)
	}
	return buildID, nil
}

func parsePublicBuildID(scanner *bufio.Scanner, appID string) (string, error) {
	stack := make([]string, 0, 8)
	pendingKey := ""
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch line {
		case "", "{":
			if line == "{" && pendingKey != "" {
				stack = append(stack, pendingKey)
				pendingKey = ""
			}
			continue
		case "}":
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			pendingKey = ""
			continue
		}

		fields := quotedVDFFields(line)
		switch len(fields) {
		case 1:
			pendingKey = fields[0]
		case 2:
			pendingKey = ""
			if fields[0] == "buildid" &&
				pathEndsWith(stack, appID, "depots", "branches", "public") {
				if _, err := strconv.ParseUint(fields[1], 10, 32); err != nil {
					return "", fmt.Errorf("invalid public build ID %q", fields[1])
				}
				return fields[1], nil
			}
		default:
			pendingKey = ""
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", errors.New("public branch build ID was not present in SteamCMD output")
}

func quotedVDFFields(line string) []string {
	fields := make([]string, 0, 2)
	for {
		start := strings.IndexByte(line, '"')
		if start < 0 {
			break
		}
		line = line[start+1:]
		end := strings.IndexByte(line, '"')
		if end < 0 {
			break
		}
		fields = append(fields, line[:end])
		line = line[end+1:]
	}
	return fields
}

func pathEndsWith(stack []string, suffix ...string) bool {
	if len(stack) < len(suffix) {
		return false
	}
	offset := len(stack) - len(suffix)
	for index := range suffix {
		if stack[offset+index] != suffix[index] {
			return false
		}
	}
	return true
}

func compareSteamBuilds(installedBuildID, availableBuildID string) (steamVersionStatus, error) {
	installed, err := strconv.ParseUint(installedBuildID, 10, 32)
	if err != nil {
		return steamVersionStatus{}, fmt.Errorf("invalid installed build ID %q", installedBuildID)
	}
	available, err := strconv.ParseUint(availableBuildID, 10, 32)
	if err != nil {
		return steamVersionStatus{}, fmt.Errorf("invalid available build ID %q", availableBuildID)
	}
	if available < installed {
		return steamVersionStatus{}, fmt.Errorf(
			"Steam public build %d is older than installed build %d; refusing to guess update status",
			available,
			installed,
		)
	}
	if available == installed {
		return steamVersionStatus{State: versionCheckCurrent}, nil
	}
	return steamVersionStatus{
		State:            versionCheckAvailable,
		AvailableVersion: availableBuildID,
		UpdateAvailable:  true,
	}, nil
}
