package palworld

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
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
	if _, err := strconv.ParseUint(installedBuildID, 10, 32); err != nil {
		s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
		return fmt.Errorf("read installed Steam build ID: %w", err)
	}
	s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckChecking})

	logDirectory := filepath.Join(s.config.InstallDir, "panel-logs")
	if err := os.MkdirAll(logDirectory, 0o700); err != nil {
		s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
		return err
	}
	logPath := filepath.Join(logDirectory, "steamcmd-version-check.log")
	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(
		ctx,
		s.config.SteamCmd,
		"+login", "anonymous",
		"+app_info_update", "1",
		"+app_info_print", palworldAppID,
		"+quit",
	)
	command.Dir = filepath.Dir(s.config.SteamCmd)
	command.Stdout, command.Stderr = logFile, logFile
	report("查询 Steam", 15, "正在启动 SteamCMD 并刷新 public 分支信息")
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
		return fmt.Errorf("start SteamCMD version check: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- command.Wait()
	}()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case runErr := <-done:
			closeErr := logFile.Close()
			if ctx.Err() != nil {
				s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
				return errors.New("SteamCMD version check timed out after 2 minutes")
			}
			if runErr != nil {
				s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
				return fmt.Errorf("SteamCMD version check: %w", runErr)
			}
			if closeErr != nil {
				s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
				return closeErr
			}
			report("解析版本", 85, "SteamCMD 查询完成，正在读取 public 分支 Build ID")
			availableBuildID, parseErr := publicBuildIDFromLog(logPath, palworldAppID)
			if parseErr != nil {
				s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
				return parseErr
			}
			status, compareErr := compareSteamBuilds(installedBuildID, availableBuildID)
			if compareErr != nil {
				s.setVersionStatus(installedBuildID, steamVersionStatus{State: versionCheckUnavailable})
				return compareErr
			}
			s.setVersionStatus(installedBuildID, status)
			if status.UpdateAvailable {
				report("检查完成", 100, fmt.Sprintf("发现新版本：Steam Build %s", availableBuildID))
			} else {
				report("检查完成", 100, "当前已是 Steam public 分支最新版本")
			}
			return nil
		case <-ticker.C:
			report("查询 Steam", 55, "SteamCMD 正在刷新应用信息；不会修改 Palworld 服务端文件")
		}
	}
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
