package dst

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"hearth/internal/panel"
)

var steamUpdateProgressPattern = regexp.MustCompile(`(?i)progress:\s*([0-9]+(?:\.[0-9]+)?)`)

var (
	shardHealthConfirmationDuration = 5 * time.Second
	recoveryHealthDuration          = 3 * time.Second
)

func (s *Service) updateServer(allowUnsafe bool, activityID string, logs []panel.LogRef) error {
	s.mu.Lock()
	wasRunning := s.masterRunning || s.cavesRunning
	s.mu.Unlock()
	if wasRunning && !allowUnsafe {
		return fmt.Errorf("%w: DST 更新需要先明确确认停止 Master/Caves", panel.ErrUnsafe)
	}
	report := func(stage string, progress int, detail string) {
		s.updateActivity(activityID, stage, progress, detail)
	}
	recoverRuntime := func(cause error) error {
		if !wasRunning {
			return cause
		}
		report("恢复原运行状态", 88, "更新未完成；正在尝试重新启动 Master/Caves")
		if startErr := s.startShards(logs[1:3]); startErr != nil {
			return fmt.Errorf("%v; restore Master/Caves after failure: %w", cause, startErr)
		}
		if healthErr := s.waitForShardsHealthy(recoveryHealthDuration); healthErr != nil {
			return fmt.Errorf("%v; restored processes did not remain healthy: %w", cause, healthErr)
		}
		return fmt.Errorf("%w; Master/Caves 已恢复到运行状态", cause)
	}

	if wasRunning {
		report("停止 Master/Caves", 12, "DST 没有 REST 安全关闭通道；正在按已确认边界终止两个分片")
		if err := s.stopShards(true); err != nil {
			return err
		}
	}
	report("创建一致性备份", 28, "分片已停止，正在备份 cluster 配置、世界与存档")
	if _, err := s.createBackup(func(stage string, progress int, detail string) {
		report(stage, 28+progress*17/100, detail)
	}, logs[0].ID); err != nil {
		return recoverRuntime(fmt.Errorf("backup DST cluster before update: %w", err))
	}

	report("SteamCMD 更新", 48, "备份已完成，正在更新 DST Dedicated Server App 343050")
	if err := s.runDSTSteamUpdate(func(stage string, progress int, detail string) {
		report(stage, 48+progress*35/100, detail)
	}, logs[0].ID); err != nil {
		s.setVersionStatus(s.installedBuildID(), steamVersionStatus{State: versionCheckUnavailable})
		return recoverRuntime(fmt.Errorf("update DST Dedicated Server: %w", err))
	}

	buildID := s.installedBuildID()
	s.setVersionStatus(buildID, steamVersionStatus{State: versionCheckCurrent})
	if !wasRunning {
		report("更新完成", 100, "DST Dedicated Server 已更新；任务前为停服状态，保持停服")
		return nil
	}
	report("恢复 Master/Caves", 86, "服务端文件已更新，正在恢复更新前的运行状态")
	if err := s.startShards(logs[1:3]); err != nil {
		return fmt.Errorf("DST updated and backup retained, but restart failed: %w", err)
	}
	report("确认分片健康", 94, "Master/Caves 已启动，正在确认两个分片持续存活")
	if err := s.waitForShardsHealthy(shardHealthConfirmationDuration); err != nil {
		return fmt.Errorf("DST updated and backup retained, but health confirmation failed: %w", err)
	}
	report("更新完成", 100, "备份、SteamCMD 更新和 Master/Caves 恢复检查已完成")
	return nil
}

func (s *Service) waitForShardsHealthy(duration time.Duration) error {
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		healthy := s.masterRunning && s.cavesRunning
		s.mu.Unlock()
		if !healthy {
			return errors.New("Master 或 Caves 进程已退出")
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

func (s *Service) runDSTSteamUpdate(report versionReporter, logID string) error {
	logPath, ok := s.TaskLogPath(logID)
	if !ok {
		return errors.New("DST update log path is unavailable")
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return err
	}
	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	report("准备 SteamCMD", 5, "正在先完成 SteamCMD 自身更新，避免与游戏更新阶段混淆")
	if err := s.runSteamVersionCommand(logFile, s.steamNoProgressTimeout(), "+quit"); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("prepare SteamCMD: %w", err)
	}
	_, _ = fmt.Fprintln(logFile, "\n[Hearth] Updating Don't Starve Together Dedicated Server app 343050.")
	report("SteamCMD 更新", 15, "正在检查、下载并校验 DST Dedicated Server 文件")
	command := exec.Command(
		s.config.SteamCmd,
		"+login", "anonymous", "+app_update", dstAppID, "validate", "+quit",
	)
	command.Dir = filepath.Dir(s.config.SteamCmd)
	command.Stdout, command.Stderr = logFile, logFile
	prepareCommand(command)
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		return err
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	lastSize, lastProgressAt := fileSize(logPath), time.Now()
	for {
		select {
		case commandErr := <-done:
			closeErr := logFile.Close()
			if commandErr != nil {
				if detail := lastLogLine(logPath); detail != "" {
					return fmt.Errorf("%w; last SteamCMD output: %s", commandErr, detail)
				}
				return commandErr
			}
			if closeErr != nil {
				return closeErr
			}
			outcome, confirmed := dstSteamUpdateResult(logPath)
			if !confirmed {
				return errors.New("SteamCMD exited without confirming DST App 343050 update completion")
			}
			if outcome == "current" {
				report("SteamCMD 更新", 100, "DST Dedicated Server 已是最新版；文件校验已完成")
			} else {
				report("SteamCMD 更新", 100, "DST Dedicated Server 下载与文件校验已完成")
			}
			return nil
		case <-ticker.C:
			size := fileSize(logPath)
			if size > lastSize {
				lastSize, lastProgressAt = size, time.Now()
			}
			line := lastLogLine(logPath)
			progress := steamProgress(line)
			if progress > 0 {
				report("SteamCMD 更新", 15+progress*75/100, line)
			} else if line != "" {
				report("SteamCMD 更新", 20, line)
			}
			if time.Since(lastProgressAt) < s.steamNoProgressTimeout() {
				continue
			}
			terminateErr := terminateCommand(command)
			waitErr := <-done
			_ = logFile.Close()
			if terminateErr != nil {
				return fmt.Errorf("SteamCMD produced no log progress for %s; terminate process tree: %w", s.steamNoProgressTimeout(), terminateErr)
			}
			if waitErr != nil {
				return fmt.Errorf("SteamCMD produced no log progress for %s and was terminated: %w", s.steamNoProgressTimeout(), waitErr)
			}
			return fmt.Errorf("SteamCMD produced no log progress for %s and was terminated", s.steamNoProgressTimeout())
		}
	}
}

func dstSteamUpdateResult(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	text := strings.ToLower(string(data))
	if strings.Contains(text, "app '343050' already up to date") {
		return "current", true
	}
	if strings.Contains(text, "success! app '343050' fully installed") {
		return "updated", true
	}
	return "", false
}

func steamProgress(line string) int {
	match := steamUpdateProgressPattern.FindStringSubmatch(line)
	if len(match) != 2 {
		return 0
	}
	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0
	}
	return max(0, min(100, int(value)))
}

func lastLogLine(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if len(data) > 16<<10 {
		data = data[len(data)-(16<<10):]
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r", ""), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		if line := strings.TrimSpace(lines[index]); line != "" {
			if len(line) > 200 {
				return line[:200] + "…"
			}
			return line
		}
	}
	return ""
}

func (s *Service) createBackup(report versionReporter, logID string) (string, error) {
	report("创建一致性备份", 5, "正在准备 DST cluster 备份目录")
	if err := os.MkdirAll(s.config.BackupDir, 0o700); err != nil {
		return "", err
	}
	temporary, err := os.CreateTemp(s.config.BackupDir, ".dst-backup-*.tmp")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	cleanup := true
	defer func() {
		_ = temporary.Close()
		if cleanup {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return "", err
	}
	archive := zip.NewWriter(temporary)
	files, bytes, err := s.writeClusterArchive(archive, report)
	if err != nil {
		_ = archive.Close()
		return "", err
	}
	if err := archive.Close(); err != nil {
		return "", err
	}
	if err := temporary.Sync(); err != nil {
		return "", err
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	name := "dst-backup-" + time.Now().Format("20060102-150405.000000000") + ".zip"
	finalPath := filepath.Join(s.config.BackupDir, name)
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return "", err
	}
	cleanup = false
	now := time.Now()
	s.mu.Lock()
	s.lastBackup = &now
	s.mu.Unlock()
	removed, freed, pruneErr := s.pruneBackups(finalPath, now)
	detail := fmt.Sprintf("已备份 %d 个文件（源数据 %.2f MiB）到 %s", files, float64(bytes)/(1<<20), filepath.Base(finalPath))
	if removed > 0 {
		detail += fmt.Sprintf("；按保留策略清理 %d 个旧备份（%.2f MiB）", removed, float64(freed)/(1<<20))
	}
	appendTaskLog(s, logID, detail+"\n")
	if pruneErr != nil {
		appendTaskLog(s, logID, "旧备份清理警告: "+pruneErr.Error()+"\n")
		detail += "；旧备份清理存在警告，当前备份仍可用"
	}
	report("创建一致性备份", 100, detail)
	return finalPath, nil
}

func (s *Service) writeClusterArchive(archive *zip.Writer, report versionReporter) (int, int64, error) {
	clusterRoot := filepath.Clean(s.config.ClusterDir)
	backupRoot := filepath.Clean(s.config.BackupDir)
	logRoot := filepath.Join(clusterRoot, "panel-logs")
	files := 0
	var bytes int64
	err := filepath.Walk(clusterRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == clusterRoot {
			return nil
		}
		if pathWithin(path, backupRoot) || pathWithin(path, logRoot) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(clusterRoot, path)
		if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("resolve DST backup path %s", path)
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relative)
		if info.IsDir() {
			header.Name += "/"
			_, err = archive.CreateHeader(header)
			return err
		}
		header.Method = zip.Deflate
		writer, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		copied, copyErr := io.Copy(writer, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		files++
		bytes += copied
		if files%100 == 0 {
			report("创建一致性备份", min(90, 10+files/10), fmt.Sprintf("已归档 %d 个文件", files))
		}
		return nil
	})
	return files, bytes, err
}

func pathWithin(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func (s *Service) pruneBackups(current string, now time.Time) (int, int64, error) {
	entries, err := os.ReadDir(s.config.BackupDir)
	if err != nil {
		return 0, 0, err
	}
	type backupFile struct {
		path string
		info os.FileInfo
	}
	files := make([]backupFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "dst-backup-") || !strings.HasSuffix(entry.Name(), ".zip") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return 0, 0, infoErr
		}
		files = append(files, backupFile{path: filepath.Join(s.config.BackupDir, entry.Name()), info: info})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].info.ModTime().After(files[j].info.ModTime()) })
	maxAge := time.Duration(s.config.BackupRetentionDays) * 24 * time.Hour
	maxBytes := s.config.BackupMaxTotalGB * (1 << 30)
	var total int64
	removed := 0
	var freed int64
	var firstErr error
	for _, file := range files {
		if file.path == current {
			total += file.info.Size()
			continue
		}
		expired := maxAge > 0 && now.Sub(file.info.ModTime()) > maxAge
		overCapacity := maxBytes > 0 && total+file.info.Size() > maxBytes
		if !expired && !overCapacity {
			total += file.info.Size()
			continue
		}
		if err := os.Remove(file.path); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		removed++
		freed += file.info.Size()
	}
	return removed, freed, firstErr
}

func appendTaskLog(s *Service, logID, text string) {
	path, ok := s.TaskLogPath(logID)
	if !ok {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	_, _ = io.WriteString(file, text)
	_ = file.Close()
}
