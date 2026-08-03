package palworld

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type InstallProgress func(stage string, progress int, detail string)

// InstallDedicatedServer runs the same guarded SteamCMD process used by the
// update path, but does not require an existing Palworld configuration. The
// caller must have received explicit administrator confirmation first.
func InstallDedicatedServer(
	steamCmd string,
	installDir string,
	logPath string,
	noProgressTimeout time.Duration,
	report InstallProgress,
) error {
	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	if report == nil {
		report = func(string, int, string) {}
	}
	for attempt := 1; attempt <= 2; attempt++ {
		if attempt > 1 {
			if _, err := fmt.Fprintln(logFile, "\n[Hearth] Retrying after a possible SteamCMD self-update."); err != nil {
				return err
			}
			report("SteamCMD 自更新", 20, "SteamCMD 可能已完成自身更新，正在自动重试")
		}
		if err := runInstallAttempt(steamCmd, installDir, logFile, logPath, noProgressTimeout, report); err != nil {
			return err
		}
		if _, completed := steamUpdateResult(logPath); completed {
			report("下载完成", 88, "Palworld Dedicated Server 已下载并通过 SteamCMD 校验")
			return nil
		}
	}
	return errors.New("SteamCMD exited successfully twice without confirming Palworld installation")
}

func runInstallAttempt(
	steamCmd string,
	installDir string,
	logFile *os.File,
	logPath string,
	noProgressTimeout time.Duration,
	report InstallProgress,
) error {
	initialSize := logFileSize(logPath)
	command := exec.Command(
		steamCmd,
		"+force_install_dir", installDir,
		"+login", "anonymous",
		"+app_update", palworldAppID, "validate",
		"+quit",
	)
	command.Dir = filepath.Dir(steamCmd)
	command.Stdout, command.Stderr = logFile, logFile
	prepareManagedCommand(command)
	report("启动 SteamCMD", 15, "正在启动 SteamCMD；安装由管理员明确发起")
	if err := command.Start(); err != nil {
		return err
	}
	report("下载服务端", 25, "正在下载并校验 Palworld Dedicated Server App 2394010")
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
				if line := latestLogLine(logPath); line != "" {
					report("下载服务端", 55, line)
				}
			}
			if time.Since(lastProgressAt) < noProgressTimeout {
				continue
			}
			terminateErr := terminateManagedCommand(command)
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
