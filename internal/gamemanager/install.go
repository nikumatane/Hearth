package gamemanager

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hearth/internal/config"
	"hearth/internal/palworld"
	"hearth/internal/panel"
)

const (
	steamCmdDownloadURL     = "https://steamcdn-a.akamaihd.net/client/installer/steamcmd.zip"
	maxSteamCmdZipBytes     = 64 << 20
	maxSteamCmdExtractBytes = 256 << 20
)

func (s *Service) InstallGame(id string, request panel.InstallGameRequest) (panel.Activity, error) {
	if id != palworldID {
		return panel.Activity{}, panel.ErrNotFound
	}
	if !request.Confirm {
		return panel.Activity{}, fmt.Errorf("%w: 安装需要管理员明确确认", panel.ErrInvalid)
	}
	installDir := filepath.Clean(strings.TrimSpace(request.InstallDir))
	steamCmdRoot := filepath.Clean(strings.TrimSpace(request.SteamCmdRoot))
	if err := validateInstallPaths(installDir, steamCmdRoot, s.configPath); err != nil {
		return panel.Activity{}, err
	}
	if err := requireEmptyInstallDirectory(installDir); err != nil {
		return panel.Activity{}, err
	}

	s.mu.Lock()
	if s.delegate != nil || s.installing {
		s.mu.Unlock()
		return panel.Activity{}, panel.ErrBusy
	}
	now := time.Now()
	activity := panel.Activity{
		ID: fmt.Sprintf("install-%d", now.UnixNano()), GameID: palworldID, Action: "install",
		Title: "安装 Palworld Dedicated Server", Detail: "管理员已确认安装目录，任务准备开始",
		Status: "running", Stage: "准备目录", Progress: 5, CreatedAt: now, UpdatedAt: now,
	}
	s.activities = append([]panel.Activity{activity}, s.activities...)
	if len(s.activities) > 20 {
		s.activities = s.activities[:20]
	}
	s.installing = true
	s.activeTask = activity.ID
	s.mu.Unlock()

	go s.runInstall(activity.ID, installDir, steamCmdRoot)
	return activity, nil
}

func (s *Service) runInstall(activityID, installDir, steamCmdRoot string) {
	report := func(stage string, progress int, detail string) {
		s.updateInstallActivity(activityID, stage, progress, detail)
	}
	fail := func(err error) {
		s.finishInstallActivity(activityID, false, "安装失败", safeErrorDetail(err))
	}

	report("准备目录", 8, "正在创建管理员选择的安装目录")
	if err := os.MkdirAll(installDir, 0o700); err != nil {
		fail(fmt.Errorf("create game install directory: %w", err))
		return
	}
	if err := os.MkdirAll(steamCmdRoot, 0o700); err != nil {
		fail(fmt.Errorf("create SteamCMD directory: %w", err))
		return
	}
	steamCmd := filepath.Join(steamCmdRoot, "steamcmd.exe")
	if !fileExists(steamCmd) {
		report("下载 SteamCMD", 10, "正在从 Valve 官方地址下载 SteamCMD")
		var err error
		steamCmd, err = downloadSteamCMD(steamCmdRoot, report)
		if err != nil {
			fail(err)
			return
		}
	} else {
		report("检查 SteamCMD", 12, "使用管理员确认目录中的现有 steamcmd.exe")
	}

	logDirectory := filepath.Join(installDir, "panel-logs")
	if err := os.MkdirAll(logDirectory, 0o700); err != nil {
		fail(fmt.Errorf("create installation log directory: %w", err))
		return
	}
	logName := "steamcmd-install-" + time.Now().Format("20060102-150405.000000000") + ".log"
	logPath := filepath.Join(logDirectory, logName)
	s.addInstallLog(activityID, logName, "Palworld 安装日志", logPath)

	s.mu.RLock()
	noProgressMinutes := defaultInt(s.config.Games.Palworld.SteamCmdNoProgressMinutes, 30)
	s.mu.RUnlock()
	if err := palworld.InstallDedicatedServer(
		steamCmd, installDir, logPath, time.Duration(noProgressMinutes)*time.Minute,
		func(stage string, progress int, detail string) { report(stage, progress, detail) },
	); err != nil {
		fail(fmt.Errorf("install Palworld with SteamCMD: %w", err))
		return
	}

	report("初始化配置", 91, "仅在配置不存在时从官方默认文件创建 PalWorldSettings.ini")
	settingsPath := filepath.Join(installDir, "Pal", "Saved", "Config", "WindowsServer", "PalWorldSettings.ini")
	if !fileExists(settingsPath) {
		if err := initializePalworldSettings(filepath.Join(installDir, "DefaultPalWorldSettings.ini"), settingsPath); err != nil {
			fail(err)
			return
		}
	}

	s.mu.Lock()
	next := s.config
	next.Management.InstallRoot = filepath.Dir(installDir)
	next.Management.SteamCmdRoot = steamCmdRoot
	next.Games.Palworld = palworldConfig(installDir, steamCmd, next.Games.Palworld)
	next.Games.Palworld.Enabled = true
	delegate, err := s.factory(next.Games.Palworld)
	if err == nil {
		err = config.Save(s.configPath, next)
	}
	if err != nil && delegate != nil {
		closeService(delegate)
	}
	if err == nil {
		s.config = next
		s.delegate = delegate
		s.initError = nil
		s.discoverLocked()
	}
	s.mu.Unlock()
	if err != nil {
		fail(fmt.Errorf("activate installed Palworld adapter: %w", err))
		return
	}
	report("健康检查", 98, "安装文件和管理路径已校验；Hearth 不会自动启动游戏")
	s.finishInstallActivity(activityID, true, "安装完成", "Palworld 已安装并接入 Hearth，服务器保持停止状态")
}

func (s *Service) updateInstallActivity(id, stage string, progress int, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.activities {
		if s.activities[index].ID == id {
			s.activities[index].Stage = stage
			s.activities[index].Progress = max(s.activities[index].Progress, min(progress, 99))
			s.activities[index].Detail = detail
			s.activities[index].UpdatedAt = time.Now()
			return
		}
	}
}

func (s *Service) addInstallLog(id, logID, label, path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logPaths[logID] = path
	for index := range s.activities {
		if s.activities[index].ID == id {
			s.activities[index].Logs = append(s.activities[index].Logs, panel.LogRef{ID: logID, Label: label})
			s.activities[index].UpdatedAt = time.Now()
			return
		}
	}
}

func (s *Service) finishInstallActivity(id string, success bool, title, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.activities {
		if s.activities[index].ID == id {
			s.activities[index].Title = title
			s.activities[index].Detail = detail
			s.activities[index].Stage = "完成"
			s.activities[index].Progress = 100
			if success {
				s.activities[index].Status = "success"
			} else {
				s.activities[index].Status = "error"
			}
			s.activities[index].UpdatedAt = time.Now()
			break
		}
	}
	s.installing = false
	s.activeTask = ""
}

func validateInstallPaths(installDir, steamCmdRoot, configPath string) error {
	for name, value := range map[string]string{"installDir": installDir, "steamCmdRoot": steamCmdRoot} {
		if value == "." || !filepath.IsAbs(value) {
			return fmt.Errorf("%w: %s 必须是绝对路径", panel.ErrInvalid, name)
		}
		volume := filepath.VolumeName(value)
		if volume != "" && filepath.Clean(value) == filepath.Clean(volume+string(filepath.Separator)) {
			return fmt.Errorf("%w: %s 不能是磁盘根目录", panel.ErrInvalid, name)
		}
	}
	if pathContains(installDir, steamCmdRoot) || pathContains(steamCmdRoot, installDir) {
		return fmt.Errorf("%w: 游戏目录和 SteamCMD 目录必须分开且不能互相包含", panel.ErrInvalid)
	}
	if configPath != "" {
		configDirectory := filepath.Dir(configPath)
		if pathContains(installDir, configDirectory) || pathContains(configDirectory, installDir) ||
			pathContains(steamCmdRoot, configDirectory) || pathContains(configDirectory, steamCmdRoot) {
			return fmt.Errorf("%w: 安装目录不能覆盖 Hearth 配置目录", panel.ErrInvalid)
		}
	}
	return nil
}

func requireEmptyInstallDirectory(path string) error {
	entries, err := os.ReadDir(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: inspect install directory: %v", panel.ErrInvalid, err)
	}
	if len(entries) > 0 {
		return fmt.Errorf("%w: 新安装目录必须为空；现有服务器请使用接管", panel.ErrInvalid)
	}
	return nil
}

func initializePalworldSettings(defaultPath, settingsPath string) error {
	data, err := os.ReadFile(defaultPath)
	if err != nil {
		return fmt.Errorf("read DefaultPalWorldSettings.ini: %w", err)
	}
	if len(data) == 0 || len(data) > 2<<20 {
		return errors.New("DefaultPalWorldSettings.ini has an invalid size")
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		return fmt.Errorf("create Palworld settings directory: %w", err)
	}
	file, err := os.OpenFile(settingsPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create PalWorldSettings.ini: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("write PalWorldSettings.ini: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync PalWorldSettings.ini: %w", err)
	}
	return file.Close()
}

func downloadSteamCMD(root string, report func(string, int, string)) (string, error) {
	if entries, err := os.ReadDir(root); err == nil && len(entries) > 0 {
		return "", errors.New("SteamCMD 目录非空但缺少 steamcmd.exe；请选择已有程序或一个空目录")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	parent := filepath.Dir(root)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", err
	}
	staging, err := os.MkdirTemp(parent, ".hearth-steamcmd-")
	if err != nil {
		return "", err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(staging)
		}
	}()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, steamCmdDownloadURL, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("download SteamCMD: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download SteamCMD: unexpected HTTP status %d", response.StatusCode)
	}
	if response.ContentLength > maxSteamCmdZipBytes {
		return "", errors.New("SteamCMD archive exceeds the safety limit")
	}
	temporary, err := os.CreateTemp(staging, ".steamcmd-*.zip")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	written, copyErr := io.Copy(temporary, io.LimitReader(response.Body, maxSteamCmdZipBytes+1))
	closeErr := temporary.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if written > maxSteamCmdZipBytes {
		return "", errors.New("SteamCMD archive exceeds the safety limit")
	}
	report("解压 SteamCMD", 12, "下载完成，正在校验 ZIP 路径并解压")
	if err := extractSteamCMDArchive(temporaryPath, staging); err != nil {
		return "", err
	}
	if err := os.Remove(temporaryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	executable := filepath.Join(staging, "steamcmd.exe")
	if !fileExists(executable) {
		return "", errors.New("SteamCMD archive did not contain steamcmd.exe")
	}
	if entries, err := os.ReadDir(root); err == nil {
		if len(entries) != 0 {
			return "", errors.New("SteamCMD 目录在下载过程中发生变化，请检查后重试")
		}
		if err := os.Remove(root); err != nil {
			return "", fmt.Errorf("prepare SteamCMD directory activation: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.Rename(staging, root); err != nil {
		return "", fmt.Errorf("activate SteamCMD directory: %w", err)
	}
	committed = true
	return filepath.Join(root, "steamcmd.exe"), nil
}

func extractSteamCMDArchive(archivePath, destinationRoot string) error {
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open SteamCMD archive: %w", err)
	}
	defer archive.Close()
	var extracted int64
	for _, entry := range archive.File {
		destination := filepath.Join(destinationRoot, filepath.FromSlash(entry.Name))
		if !pathContains(destinationRoot, destination) {
			return errors.New("SteamCMD archive contains an unsafe path")
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(destination, 0o700); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return err
		}
		source, err := entry.Open()
		if err != nil {
			return err
		}
		target, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			source.Close()
			return err
		}
		copied, copyErr := io.Copy(target, io.LimitReader(source, maxSteamCmdExtractBytes-extracted+1))
		extracted += copied
		closeTargetErr := target.Close()
		closeSourceErr := source.Close()
		if copyErr != nil {
			return copyErr
		}
		if extracted > maxSteamCmdExtractBytes {
			return errors.New("SteamCMD archive expands beyond the safety limit")
		}
		if closeTargetErr != nil {
			return closeTargetErr
		}
		if closeSourceErr != nil {
			return closeSourceErr
		}
	}
	return nil
}

func pathContains(parent, child string) bool {
	relative, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(child))
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}
