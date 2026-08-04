//go:build windows

package appupdate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func RunUpdater(plan Plan) error {
	logFile, err := os.OpenFile(plan.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	logLine := func(format string, args ...any) {
		_, _ = fmt.Fprintf(logFile, "%s "+format+"\n", append([]any{time.Now().Format(time.RFC3339)}, args...)...)
		_ = logFile.Sync()
	}
	finish := func(result Result) error {
		result.ActorCredentialID = plan.ActorCredentialID
		result.ActorRole = plan.ActorRole
		result.ActorIP = plan.ActorIP
		result.CompletedAt = time.Now()
		if err := writeJSONAtomic(plan.ResultFile, result); err != nil {
			return err
		}
		logLine("%s: %s", result.Stage, result.Message)
		return nil
	}
	abort := func(stage string, updateErr error) error {
		if restartErr := runScheduledTask("/Run", "/TN", plan.TaskName); restartErr != nil {
			updateErr = fmt.Errorf("%w; old panel restart also failed: %v", updateErr, restartErr)
		}
		return finishFailure(finish, plan, stage, updateErr)
	}

	logLine("waiting for Hearth process %d to exit", plan.ParentPID)
	if err := waitForProcessExit(plan.ParentPID, 30*time.Second); err != nil {
		_ = runScheduledTask("/End", "/TN", plan.TaskName)
		if err := waitForProcessExit(plan.ParentPID, 10*time.Second); err != nil {
			return abort("等待旧进程退出失败", err)
		}
	}

	backupDir := filepath.Join(plan.StageDir, "rollback")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return abort("创建回滚目录失败", err)
	}
	backupHearth := filepath.Join(backupDir, "hearth.exe")
	backupUpdater := filepath.Join(backupDir, "hearth-updater.exe")
	installedUpdater := filepath.Join(plan.InstallDir, "hearth-updater.exe")
	if err := copyFile(plan.TargetExecutable, backupHearth); err != nil {
		return abort("创建回滚副本失败", err)
	}
	updaterExisted := false
	if info, statErr := os.Stat(installedUpdater); statErr == nil && info.Mode().IsRegular() {
		updaterExisted = true
		if err := copyFile(installedUpdater, backupUpdater); err != nil {
			return abort("创建更新器回滚副本失败", err)
		}
	}

	logLine("activating Hearth %s", plan.Version)
	if err := replaceFile(filepath.Join(plan.StageDir, "hearth.exe"), plan.TargetExecutable); err != nil {
		return abort("替换 Hearth 失败", err)
	}
	if err := replaceFile(filepath.Join(plan.StageDir, "hearth-updater.exe"), installedUpdater); err != nil {
		if restoreErr := replaceFile(backupHearth, plan.TargetExecutable); restoreErr != nil {
			err = fmt.Errorf("%w; restoring the old Hearth executable also failed: %v", err, restoreErr)
		}
		return abort("替换更新器失败", err)
	}
	if err := runScheduledTask("/Run", "/TN", plan.TaskName); err == nil {
		err = waitForHealth(plan.HealthURL, plan.Version, 60*time.Second)
	}
	if err == nil {
		return finish(Result{State: "succeeded", Stage: "面板更新完成", Version: plan.Version, PreviousVersion: plan.PreviousVersion, Message: "新版本已通过健康检查；游戏服务器未受影响"})
	}

	logLine("new version health check failed, rolling back: %v", err)
	_ = runScheduledTask("/End", "/TN", plan.TaskName)
	_ = waitForHealthDown(plan.HealthURL, 15*time.Second)
	mainRollbackErr := replaceFileWithRetry(backupHearth, plan.TargetExecutable, 15*time.Second)
	var updaterRollbackErr error
	if updaterExisted {
		updaterRollbackErr = replaceFileWithRetry(backupUpdater, installedUpdater, 15*time.Second)
	} else {
		updaterRollbackErr = os.Remove(installedUpdater)
		if errors.Is(updaterRollbackErr, os.ErrNotExist) {
			updaterRollbackErr = nil
		}
	}
	var restartErr error
	if mainRollbackErr == nil {
		restartErr = runScheduledTask("/Run", "/TN", plan.TaskName)
	}
	if mainRollbackErr == nil && restartErr == nil {
		restartErr = waitForHealth(plan.HealthURL, plan.PreviousVersion, 60*time.Second)
	}
	if mainRollbackErr != nil || restartErr != nil || updaterRollbackErr != nil {
		message := fmt.Sprintf("新版本启动失败（%v），自动回滚不完整（主程序：%v；更新器：%v；旧版本健康检查：%v）；请使用安装包人工恢复", err, mainRollbackErr, updaterRollbackErr, restartErr)
		_ = finish(Result{State: "failed", Stage: "自动回滚失败", Version: plan.Version, PreviousVersion: plan.PreviousVersion, Message: message})
		return errors.New(message)
	}
	return finish(Result{State: "rolled_back", Stage: "已自动回滚", Version: plan.Version, PreviousVersion: plan.PreviousVersion, Message: fmt.Sprintf("新版本未通过健康检查，已恢复 %s：%v", plan.PreviousVersion, err)})
}

func finishFailure(finish func(Result) error, plan Plan, stage string, err error) error {
	_ = finish(Result{State: "failed", Stage: stage, Version: plan.Version, PreviousVersion: plan.PreviousVersion, Message: err.Error()})
	return err
}

func runScheduledTask(arguments ...string) error {
	command := exec.Command("schtasks.exe", arguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("schtasks %s: %w (%s)", strings.Join(arguments, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func waitForProcessExit(pid int, timeout time.Duration) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}
	done := make(chan error, 1)
	go func() { _, waitErr := process.Wait(); done <- waitErr }()
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return errors.New("Hearth process did not exit before the update timeout")
	}
}

func replaceFile(source, target string) error {
	temporary := target + ".new"
	predecessor := target + ".replacing"
	_ = os.Remove(temporary)
	if _, err := os.Stat(predecessor); err == nil {
		if _, targetErr := os.Stat(target); errors.Is(targetErr, os.ErrNotExist) {
			if restoreErr := os.Rename(predecessor, target); restoreErr != nil {
				return restoreErr
			}
		} else if removeErr := os.Remove(predecessor); removeErr != nil {
			return removeErr
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := copyFile(source, temporary); err != nil {
		return err
	}
	targetExists := false
	if info, err := os.Stat(target); err == nil && info.Mode().IsRegular() {
		targetExists = true
		if err := os.Rename(target, predecessor); err != nil {
			_ = os.Remove(temporary)
			return err
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, target); err != nil {
		_ = os.Remove(temporary)
		if targetExists {
			_ = os.Rename(predecessor, target)
		}
		return err
	}
	_ = os.Remove(predecessor)
	return nil
}

func replaceFileWithRetry(source, target string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		if last = replaceFile(source, target); last == nil {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return last
}

func copyFile(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	if syncErr := output.Sync(); copyErr == nil {
		copyErr = syncErr
	}
	if closeErr := output.Close(); copyErr == nil {
		copyErr = closeErr
	}
	return copyErr
}

func waitForHealth(url, expected string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	var last string
	for time.Now().Before(deadline) {
		response, err := client.Get(url)
		if err == nil {
			var document struct {
				Status  string `json:"status"`
				Version string `json:"version"`
			}
			decodeErr := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&document)
			response.Body.Close()
			if response.StatusCode == http.StatusOK && decodeErr == nil && document.Status == "ok" && document.Version == expected {
				return nil
			}
			last = fmt.Sprintf("HTTP %d, version %q", response.StatusCode, document.Version)
		} else {
			last = err.Error()
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("health check timed out: %s", last)
}

func waitForHealthDown(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: time.Second}
	for time.Now().Before(deadline) {
		response, err := client.Get(url)
		if err != nil {
			return nil
		}
		response.Body.Close()
		time.Sleep(250 * time.Millisecond)
	}
	return errors.New("Hearth remained healthy after scheduled task stop")
}
