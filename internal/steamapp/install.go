package steamapp

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type InstallProgress func(stage string, progress int, detail string)

type InstallSpec struct {
	AppID       string
	ProductName string
}

var (
	installProgressPattern   = regexp.MustCompile(`(?i)progress:\s*([0-9]+(?:\.[0-9]+)?)`)
	bootstrapProgressPattern = regexp.MustCompile(`\[\s*([0-9]{1,3})%\]`)
)

// InstallDedicatedServer installs one Steam dedicated-server app into
// SteamCMD's standard library. It never chooses a custom force_install_dir.
func InstallDedicatedServer(
	steamCmd string,
	logPath string,
	noProgressTimeout time.Duration,
	spec InstallSpec,
	report InstallProgress,
) error {
	if strings.TrimSpace(spec.AppID) == "" || strings.TrimSpace(spec.ProductName) == "" {
		return errors.New("Steam app installation specification is incomplete")
	}
	if report == nil {
		report = func(string, int, string) {}
	}
	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()

	for attempt := 1; attempt <= 2; attempt++ {
		if attempt > 1 {
			report("等待 SteamCMD 退出", 20, "SteamCMD 可能完成了自更新，正在等待派生进程退出后重试")
			if err := waitForSteamCMDExit(30 * time.Second); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(logFile, "\n[Hearth] Retrying after a possible SteamCMD self-update."); err != nil {
				return err
			}
			report("SteamCMD 自更新", 20, "SteamCMD 可能已完成自身更新，正在自动重试")
		}
		offset := installLogSize(logPath)
		if err := runInstallAttempt(steamCmd, logFile, logPath, noProgressTimeout, spec, report); err != nil {
			if attempt == 1 && steamCMDSelfUpdateCompletedSince(logPath, offset) {
				continue
			}
			return err
		}
		if installCompletedSince(logPath, offset, spec.AppID) {
			report("等待 SteamCMD 退出", 86, "下载已完成，正在等待 SteamCMD 派生进程完全退出")
			if err := waitForSteamCMDExit(30 * time.Second); err != nil {
				return err
			}
			report("下载完成", 88, spec.ProductName+" 已下载并通过 SteamCMD 校验")
			return nil
		}
	}
	return fmt.Errorf("SteamCMD exited successfully twice without confirming App %s installation", spec.AppID)
}

func runInstallAttempt(
	steamCmd string,
	logFile *os.File,
	logPath string,
	noProgressTimeout time.Duration,
	spec InstallSpec,
	report InstallProgress,
) error {
	initialSize := installLogSize(logPath)
	command := exec.Command(steamCmd, "+login", "anonymous", "+app_update", spec.AppID, "validate", "+quit")
	command.Dir = filepath.Dir(steamCmd)
	command.Stdout, command.Stderr = logFile, logFile
	prepareInstallCommand(command)
	report("启动 SteamCMD", 15, "正在启动 SteamCMD；安装由管理员明确发起")
	if err := command.Start(); err != nil {
		return err
	}
	report("下载服务端", 25, fmt.Sprintf("正在下载并校验 %s App %s", spec.ProductName, spec.AppID))
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	pollInterval := 2 * time.Second
	if noProgressTimeout < 8*time.Second {
		pollInterval = max(50*time.Millisecond, noProgressTimeout/4)
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	lastSize, lastProgressAt, progress := initialSize, time.Now(), 25
	for {
		select {
		case err := <-done:
			return err
		case <-ticker.C:
			size := installLogSize(logPath)
			if size > lastSize {
				lastSize, lastProgressAt = size, time.Now()
				if line := latestInstallLogLine(logPath); line != "" {
					if parsed, ok := InstallProgressFromSteamLog(line); ok {
						progress = max(progress, parsed)
					}
					report("下载服务端", progress, line)
				}
			}
			if time.Since(lastProgressAt) < noProgressTimeout {
				continue
			}
			terminateErr := terminateInstallCommand(command)
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

func InstallProgressFromSteamLog(line string) (int, bool) {
	if match := installProgressPattern.FindStringSubmatch(line); len(match) == 2 {
		value, err := strconv.ParseFloat(match[1], 64)
		if err == nil && value >= 0 && value <= 100 {
			return 45 + int(math.Round(value*40/100)), true
		}
	}
	if match := bootstrapProgressPattern.FindStringSubmatch(line); len(match) == 2 {
		value, err := strconv.Atoi(match[1])
		if err == nil && value >= 0 && value <= 100 {
			return 25 + int(math.Round(float64(value)*20/100)), true
		}
	}
	return 0, false
}

func installCompletedSince(path string, offset int64, appID string) bool {
	text := strings.ToLower(readInstallLogSince(path, offset, 256<<10))
	marker := "success! app '" + strings.ToLower(appID) + "'"
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, marker) &&
			(strings.Contains(line, "fully installed") || strings.Contains(line, "already up to date")) {
			return true
		}
	}
	return false
}

func steamCMDSelfUpdateCompletedSince(path string, offset int64) bool {
	return strings.Contains(strings.ToLower(readInstallLogSince(path, offset, 256<<10)), "update complete, launching")
}

func readInstallLogSince(path string, offset, limit int64) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() <= offset {
		return ""
	}
	start := max(offset, info.Size()-limit)
	data := make([]byte, info.Size()-start)
	read, err := file.ReadAt(data, start)
	if err != nil && !errors.Is(err, io.EOF) {
		return ""
	}
	return string(data[:read])
}

func latestInstallLogLine(path string) string {
	text := strings.ReplaceAll(readInstallLogSince(path, 0, 16<<10), "\r", "")
	lines := strings.Split(text, "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		line := strings.TrimSpace(lines[index])
		if line == "" {
			continue
		}
		if len(line) > 200 {
			line = line[:200] + "…"
		}
		return line
	}
	return ""
}

func installLogSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}
