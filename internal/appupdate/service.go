package appupdate

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"hearth/internal/buildinfo"
	"hearth/internal/config"
	"hearth/internal/panel"
)

const (
	Repository       = "nikumatane/Hearth"
	defaultAPIBase   = "https://api.github.com"
	maxReleaseBody   = 4 << 20
	maxDownloadSize  = 128 << 20
	maxExtractedSize = 256 << 20
	updateTaskName   = "Hearth"
	updateResultName = "panel-update-result.json"
	updatePlanName   = "panel-update-plan.json"
	updateLogName    = "panel-update.log"
	githubTokenEnv   = "HEARTH_GITHUB_TOKEN"
	requestUserAgent = "Hearth-panel-updater/1"
	metadataTimeout  = 30 * time.Second
	downloadTimeout  = 10 * time.Minute
	headerTimeout    = 30 * time.Second
)

type Options struct {
	APIBase            string
	HTTPClient         *http.Client
	DownloadHTTPClient *http.Client
	Executable         string
	Shutdown           func()
	RuntimeGOOS        string
	Now                func() time.Time
	Launch             func(string) error
}

type Service struct {
	mu             sync.RWMutex
	config         config.Config
	client         *http.Client
	downloadClient *http.Client
	apiBase        string
	executable     string
	shutdown       func()
	runtimeOS      string
	now            func() time.Time
	launch         func(string) error
	status         panel.PanelUpdateStatus
	release        *release
	lastResult     *Result
	resultClaimed  bool
}

type release struct {
	Version string
	Tag     string
	Assets  map[string]releaseAsset
}

type releaseAsset struct {
	Name   string
	URL    string
	Size   int64
	Digest string
}

type githubRelease struct {
	TagName    string        `json:"tag_name"`
	Draft      bool          `json:"draft"`
	Prerelease bool          `json:"prerelease"`
	Assets     []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}

func New(cfg config.Config, _ string, options Options) (*Service, error) {
	executable := options.Executable
	if executable == "" {
		var err error
		executable, err = os.Executable()
		if err != nil {
			return nil, fmt.Errorf("resolve Hearth executable: %w", err)
		}
	}
	apiBase := strings.TrimRight(options.APIBase, "/")
	if apiBase == "" {
		apiBase = defaultAPIBase
	}
	client := options.HTTPClient
	if client == nil {
		client = newHTTPClient(metadataTimeout)
	}
	downloadClient := options.DownloadHTTPClient
	if downloadClient == nil {
		downloadClient = newHTTPClient(downloadTimeout)
	}
	runtimeOS := options.RuntimeGOOS
	if runtimeOS == "" {
		runtimeOS = runtime.GOOS
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	launch := options.Launch
	if launch == nil {
		launch = launchUpdateHelper
	}
	s := &Service{
		config: cfg, client: client, downloadClient: downloadClient, apiBase: apiBase,
		executable: executable, shutdown: options.Shutdown, runtimeOS: runtimeOS,
		now: now, launch: launch,
	}
	s.status = panel.PanelUpdateStatus{
		CurrentVersion:  buildinfo.Version,
		Channel:         cfg.Update.Channel,
		State:           "idle",
		Stage:           "等待检查",
		CanApply:        runtimeOS == "windows",
		TokenConfigured: s.tokenConfigured(),
	}
	if err := s.refreshResultLocked(); err != nil {
		s.status.State = "failed"
		s.status.Stage = "更新结果读取失败"
		s.status.Message = err.Error()
	}
	return s, nil
}

func newHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = headerTimeout
	return &http.Client{Timeout: timeout, Transport: transport, CheckRedirect: validateGitHubRedirect}
}

func validateGitHubRedirect(request *http.Request, _ []*http.Request) error {
	host := strings.ToLower(request.URL.Hostname())
	if request.URL.Scheme != "https" || (host != "github.com" && host != "api.github.com" && !strings.HasSuffix(host, ".githubusercontent.com")) {
		return errors.New("GitHub asset redirect left the trusted HTTPS origins")
	}
	return nil
}

func (s *Service) ConsumeUpdateResult() *panel.PanelUpdateResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastResult == nil || s.lastResult.Consumed || s.resultClaimed {
		return nil
	}
	s.resultClaimed = true
	return &panel.PanelUpdateResult{
		State: s.lastResult.State, Version: s.lastResult.Version,
		PreviousVersion: s.lastResult.PreviousVersion, Message: s.lastResult.Message,
		ActorCredentialID: s.lastResult.ActorCredentialID,
		ActorRole:         s.lastResult.ActorRole, ActorIP: s.lastResult.ActorIP,
	}
}

func (s *Service) CompleteUpdateResultImport(success bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastResult == nil || s.lastResult.Consumed || !s.resultClaimed {
		return nil
	}
	if !success {
		s.resultClaimed = false
		return nil
	}
	completed := *s.lastResult
	completed.Consumed = true
	if err := writeJSONAtomic(filepath.Join(s.config.Update.StagingDir, updateResultName), &completed); err != nil {
		s.resultClaimed = false
		return err
	}
	s.lastResult.Consumed = true
	s.resultClaimed = false
	return nil
}

func (s *Service) UpdateStatus() panel.PanelUpdateStatus {
	s.mu.Lock()
	if err := s.refreshResultLocked(); err != nil && s.status.State != "checking" && s.status.State != "preparing" {
		s.status.State = "failed"
		s.status.Stage = "更新结果读取失败"
		s.status.Progress = 0
		s.status.Message = err.Error()
	}
	status := s.status
	s.mu.Unlock()
	status.TokenConfigured = s.tokenConfigured()
	return status
}

func (s *Service) refreshResultLocked() error {
	result, err := s.readResult()
	if err != nil || result == nil {
		return err
	}
	if result.Consumed {
		if !sameResult(s.lastResult, result) {
			s.lastResult = result
			s.resultClaimed = false
		}
		return nil
	}
	if !resultMatchesRunningVersion(result, buildinfo.Version) || sameResult(s.lastResult, result) {
		return nil
	}
	s.lastResult = result
	s.resultClaimed = false
	s.status.State = result.State
	s.status.Stage = result.Stage
	s.status.Message = result.Message
	s.status.Progress = 100
	s.status.LatestVersion = result.Version
	s.status.UpdateAvailable = false
	return nil
}

func sameResult(left, right *Result) bool {
	return left != nil && right != nil &&
		left.State == right.State && left.Version == right.Version &&
		left.PreviousVersion == right.PreviousVersion && left.CompletedAt.Equal(right.CompletedAt)
}

func resultMatchesRunningVersion(result *Result, currentVersion string) bool {
	if result == nil {
		return false
	}
	switch result.State {
	case "succeeded":
		return result.Version == currentVersion
	case "rolled_back":
		return result.PreviousVersion == currentVersion
	case "failed":
		return result.Version == currentVersion || result.PreviousVersion == currentVersion
	default:
		return false
	}
}

func (s *Service) CheckForUpdate(ctx context.Context) (panel.PanelUpdateStatus, error) {
	s.mu.Lock()
	if s.status.State == "checking" || s.status.State == "preparing" {
		s.mu.Unlock()
		return panel.PanelUpdateStatus{}, panel.ErrBusy
	}
	s.status.State, s.status.Stage, s.status.Progress = "checking", "查询 GitHub Release", 10
	s.status.Message = ""
	s.mu.Unlock()

	rel, err := s.fetchRelease(ctx)
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.CheckedAt = &now
	if err != nil {
		s.status.State, s.status.Stage, s.status.Progress = "failed", "版本检查失败", 0
		s.status.Message = friendlyCheckError(err, s.tokenConfigured())
		return s.status, errors.New(s.status.Message)
	}
	s.release = rel
	s.status.LatestVersion = rel.Version
	s.status.UpdateAvailable = compareVersions(rel.Version, buildinfo.Version) > 0
	s.status.State, s.status.Progress = "ready", 100
	if s.status.UpdateAvailable {
		s.status.Stage = "发现新版本"
		s.status.Message = "已校验发布来源与制品元数据"
	} else {
		s.status.Stage = "已是最新版本"
		s.status.Message = "当前版本不低于所选通道的最新版本"
	}
	return s.status, nil
}

func (s *Service) ApplyUpdate(request panel.PanelUpdateRequest) (panel.PanelUpdateStatus, error) {
	if !request.Confirm {
		return panel.PanelUpdateStatus{}, fmt.Errorf("%w: 安装面板更新需要管理员明确确认", panel.ErrInvalid)
	}
	s.mu.Lock()
	if s.runtimeOS != "windows" {
		s.mu.Unlock()
		return panel.PanelUpdateStatus{}, fmt.Errorf("%w: 当前版本仅支持在 Windows 上安装面板更新", panel.ErrInvalid)
	}
	if s.status.State == "checking" || s.status.State == "preparing" {
		s.mu.Unlock()
		return panel.PanelUpdateStatus{}, panel.ErrBusy
	}
	if s.release == nil || !s.status.UpdateAvailable || request.Version != s.release.Version {
		s.mu.Unlock()
		return panel.PanelUpdateStatus{}, fmt.Errorf("%w: 版本检查结果已变化，请重新检查", panel.ErrInvalid)
	}
	rel := *s.release
	s.status.State, s.status.Stage, s.status.Progress = "preparing", "下载更新包", 15
	s.status.Message = "Hearth 将在校验完成后短暂重启；游戏服务器不会停止"
	status := s.status
	s.mu.Unlock()

	go s.prepareAndLaunch(rel, request)
	return status, nil
}

func (s *Service) prepareAndLaunch(rel release, request panel.PanelUpdateRequest) {
	fail := func(stage string, err error) {
		result := &Result{
			State: "failed", Stage: stage, Version: rel.Version, PreviousVersion: buildinfo.Version,
			Message: err.Error(), CompletedAt: s.now(), ActorCredentialID: request.ActorCredentialID,
			ActorRole: request.ActorRole, ActorIP: request.ActorIP,
		}
		s.mu.Lock()
		s.status.State, s.status.Stage, s.status.Progress = "failed", stage, 0
		s.status.Message = err.Error()
		s.lastResult = result
		s.mu.Unlock()
		_ = writeJSONAtomic(filepath.Join(s.config.Update.StagingDir, updateResultName), result)
	}
	stage, err := s.downloadAndStage(context.Background(), rel, request)
	if err != nil {
		fail("更新准备失败", err)
		return
	}
	s.mu.Lock()
	s.status.Stage, s.status.Progress = "启动独立更新器", 90
	s.mu.Unlock()
	if err := s.launch(filepath.Join(stage, "hearth-updater.exe")); err != nil {
		fail("更新器启动失败", err)
		return
	}
	s.mu.Lock()
	s.status.Stage, s.status.Progress = "等待 Hearth 重启", 95
	s.status.Message = "更新器已接管；若新版本健康检查失败，将自动恢复当前版本"
	s.mu.Unlock()
	if s.shutdown != nil {
		time.AfterFunc(750*time.Millisecond, s.shutdown)
	}
}

func (s *Service) fetchRelease(ctx context.Context) (*release, error) {
	path := "/repos/" + Repository + "/releases/latest"
	if s.config.Update.Channel == "prerelease" {
		path = "/repos/" + Repository + "/releases?per_page=100"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.apiBase+path, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	s.authorize(request)
	response, err := s.client.Do(request)
	if err != nil {
		return nil, safeRequestError("request GitHub releases", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub releases returned HTTP %d", response.StatusCode)
	}
	reader := io.LimitReader(response.Body, maxReleaseBody+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if len(data) > maxReleaseBody {
		return nil, errors.New("GitHub release response is too large")
	}
	var document githubRelease
	if s.config.Update.Channel == "prerelease" {
		var releases []githubRelease
		if err := json.Unmarshal(data, &releases); err != nil {
			return nil, fmt.Errorf("decode GitHub releases: %w", err)
		}
		found := false
		selectedVersion := ""
		for _, candidate := range releases {
			if candidate.Draft {
				continue
			}
			version, ok := parseVersion(candidate.TagName)
			if !ok {
				continue
			}
			if !found || compareVersions(version, selectedVersion) > 0 {
				document = candidate
				selectedVersion = version
				found = true
			}
		}
		if !found {
			return nil, errors.New("no published semantic release found")
		}
	} else if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("decode GitHub release: %w", err)
	}
	if document.Draft || (s.config.Update.Channel == "stable" && document.Prerelease) {
		return nil, errors.New("GitHub returned a release outside the selected channel")
	}
	version, ok := parseVersion(document.TagName)
	if !ok {
		return nil, fmt.Errorf("release tag %q is not a supported semantic version", document.TagName)
	}
	assets := make(map[string]releaseAsset, len(document.Assets))
	for _, asset := range document.Assets {
		if _, exists := assets[asset.Name]; exists {
			return nil, fmt.Errorf("release contains duplicate asset %s", asset.Name)
		}
		if err := validateAssetURL(s.apiBase, asset.URL); err != nil {
			return nil, fmt.Errorf("release asset %s: %w", asset.Name, err)
		}
		assets[asset.Name] = releaseAsset{Name: asset.Name, URL: asset.URL, Size: asset.Size, Digest: asset.Digest}
	}
	zipName := "hearth-windows-amd64-v" + version + ".zip"
	checksumName := zipName + ".sha256"
	for _, name := range []string{zipName, checksumName} {
		asset, exists := assets[name]
		if !exists || asset.URL == "" || asset.Size <= 0 {
			return nil, fmt.Errorf("release is missing required asset %s", name)
		}
	}
	if !validGitHubSHA256Digest(assets[zipName].Digest) {
		return nil, fmt.Errorf("release asset %s has no trusted SHA-256 digest", zipName)
	}
	return &release{Version: version, Tag: document.TagName, Assets: assets}, nil
}

func validateAssetURL(apiBase, value string) error {
	base, baseErr := url.Parse(apiBase)
	asset, assetErr := url.Parse(value)
	if baseErr != nil || assetErr != nil || asset.Scheme != base.Scheme || !strings.EqualFold(asset.Host, base.Host) || asset.User != nil || asset.Fragment != "" || asset.RawQuery != "" {
		return errors.New("download URL is outside the configured GitHub API origin")
	}
	if apiBase == defaultAPIBase && !strings.HasPrefix(asset.Path, "/repos/"+Repository+"/releases/assets/") {
		return errors.New("download URL is outside the official Hearth release assets")
	}
	return nil
}

func (s *Service) downloadAndStage(ctx context.Context, rel release, request panel.PanelUpdateRequest) (string, error) {
	if strings.TrimSpace(s.config.Update.StagingDir) == "" {
		return "", errors.New("update staging directory is not configured")
	}
	stage := filepath.Join(s.config.Update.StagingDir, "v"+rel.Version)
	if err := os.RemoveAll(stage); err != nil {
		return "", fmt.Errorf("reset update staging directory: %w", err)
	}
	if err := os.MkdirAll(stage, 0o700); err != nil {
		return "", fmt.Errorf("create update staging directory: %w", err)
	}
	zipName := "hearth-windows-amd64-v" + rel.Version + ".zip"
	zipPath := filepath.Join(stage, zipName)
	if err := s.downloadAssetWithProgress(ctx, rel.Assets[zipName], zipPath, maxDownloadSize, s.setDownloadProgress); err != nil {
		return "", err
	}
	s.setProgress("校验 SHA-256", 45, "更新包下载完成，正在核对 GitHub 摘要与随包校验文件")
	digest, err := fileSHA256(zipPath)
	if err != nil {
		return "", err
	}
	expected := strings.TrimPrefix(strings.ToLower(rel.Assets[zipName].Digest), "sha256:")
	if digest != expected {
		return "", errors.New("更新包摘要与 GitHub Release 元数据不一致")
	}
	checksumName := zipName + ".sha256"
	checksumPath := filepath.Join(stage, checksumName)
	if err := s.downloadAsset(ctx, rel.Assets[checksumName], checksumPath, 64<<10); err != nil {
		return "", err
	}
	checksum, err := readChecksum(checksumPath, zipName)
	if err != nil {
		return "", err
	}
	if checksum != digest {
		return "", errors.New("更新包摘要与随包校验文件不一致")
	}
	s.setProgress("安全解压更新包", 60, "摘要校验通过，正在安全解压更新文件")
	if err := extractPackage(zipPath, stage); err != nil {
		return "", err
	}
	for _, name := range []string{"hearth.exe", "hearth-updater.exe", "VERSION"} {
		if info, err := os.Stat(filepath.Join(stage, name)); err != nil || !info.Mode().IsRegular() {
			return "", fmt.Errorf("更新包缺少 %s", name)
		}
	}
	versionData, err := os.ReadFile(filepath.Join(stage, "VERSION"))
	if err != nil || strings.TrimSpace(string(versionData)) != rel.Version {
		return "", errors.New("更新包内 VERSION 与 Release 版本不一致")
	}
	s.setProgress("生成回滚计划", 78, "更新文件验证通过，正在准备独立更新器与自动回滚")
	plan, err := s.newPlan(stage, rel.Version, request)
	if err != nil {
		return "", err
	}
	if err := writeJSONAtomic(filepath.Join(stage, updatePlanName), plan); err != nil {
		return "", fmt.Errorf("write update plan: %w", err)
	}
	return stage, nil
}

func (s *Service) downloadAsset(ctx context.Context, asset releaseAsset, path string, limit int64) error {
	return s.downloadAssetWithProgress(ctx, asset, path, limit, nil)
}

func (s *Service) downloadAssetWithProgress(ctx context.Context, asset releaseAsset, path string, limit int64, progress func(int64, int64)) error {
	if asset.Size > limit {
		return fmt.Errorf("release asset %s exceeds the size limit", asset.Name)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/octet-stream")
	s.authorize(request)
	response, err := s.downloadClient.Do(request)
	if err != nil {
		return safeRequestError("download "+asset.Name, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s returned HTTP %d", asset.Name, response.StatusCode)
	}
	temporaryPath := path + ".part"
	_ = os.Remove(temporaryPath)
	file, err := os.OpenFile(temporaryPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	completed := false
	defer func() {
		_ = file.Close()
		if !completed {
			_ = os.Remove(temporaryPath)
		}
	}()
	reader := io.Reader(io.LimitReader(response.Body, limit+1))
	if progress != nil {
		reader = &progressReader{reader: reader, total: asset.Size, progress: progress}
	}
	written, copyErr := io.Copy(file, reader)
	closeErr := file.Close()
	if copyErr != nil {
		return safeRequestError("download "+asset.Name, copyErr)
	}
	if closeErr != nil {
		return closeErr
	}
	if written > limit || written != asset.Size {
		return fmt.Errorf("downloaded size for %s is invalid", asset.Name)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("activate downloaded asset %s: %w", asset.Name, err)
	}
	completed = true
	return nil
}

type progressReader struct {
	reader   io.Reader
	total    int64
	written  int64
	progress func(int64, int64)
}

func (r *progressReader) Read(buffer []byte) (int, error) {
	count, err := r.reader.Read(buffer)
	if count > 0 {
		r.written += int64(count)
		r.progress(r.written, r.total)
	}
	return count, err
}

func safeRequestError(action string, err error) error {
	var requestError *url.Error
	if errors.As(err, &requestError) {
		return fmt.Errorf("%s: %v", action, requestError.Err)
	}
	return fmt.Errorf("%s: %v", action, err)
}

func (s *Service) authorize(request *http.Request) {
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", requestUserAgent)
	if token := s.token(); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
}

func (s *Service) tokenConfigured() bool { return s.token() != "" }

func (s *Service) token() string {
	if token := strings.TrimSpace(os.Getenv(githubTokenEnv)); token != "" {
		return token
	}
	path := strings.TrimSpace(s.config.Update.TokenFile)
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 4096 {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(string(data), "\uFEFF"))
}

func (s *Service) setProgress(stage string, progress int, message string) {
	s.mu.Lock()
	s.status.Stage, s.status.Progress, s.status.Message = stage, progress, message
	s.mu.Unlock()
}

func (s *Service) setDownloadProgress(written, total int64) {
	if total <= 0 {
		return
	}
	progress := 15 + int(written*25/total)
	if progress > 40 {
		progress = 40
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status.State != "preparing" || progress <= s.status.Progress {
		return
	}
	s.status.Stage = "下载更新包"
	s.status.Progress = progress
	s.status.Message = fmt.Sprintf("已下载 %.2f / %.2f MiB；完成后将自动校验并重启 Hearth", float64(written)/(1<<20), float64(total)/(1<<20))
}

func friendlyCheckError(err error, token bool) string {
	if !token && strings.Contains(err.Error(), "HTTP 404") {
		return "当前仓库仍为私有仓库，请在 github-token.txt 中配置仅 Contents: Read 的 GitHub Token"
	}
	return err.Error()
}

func parseVersion(value string) (string, bool) {
	value = strings.TrimSpace(strings.TrimPrefix(value, "v"))
	main := strings.SplitN(value, "-", 2)[0]
	parts := strings.Split(main, ".")
	if len(parts) != 3 {
		return "", false
	}
	for _, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return "", false
		}
		if _, err := strconv.ParseUint(part, 10, 32); err != nil {
			return "", false
		}
	}
	if strings.ContainsAny(value, "/\\") {
		return "", false
	}
	if pieces := strings.SplitN(value, "-", 2); len(pieces) == 2 {
		if pieces[1] == "" || strings.Contains(pieces[1], "..") {
			return "", false
		}
		for _, char := range pieces[1] {
			if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '.' || char == '-') {
				return "", false
			}
		}
		for _, identifier := range strings.Split(pieces[1], ".") {
			if identifier == "" {
				return "", false
			}
			if _, err := strconv.ParseUint(identifier, 10, 64); err == nil && len(identifier) > 1 && identifier[0] == '0' {
				return "", false
			}
		}
	}
	return value, true
}

func compareVersions(left, right string) int {
	l, lok := versionNumbers(left)
	r, rok := versionNumbers(right)
	if !lok || !rok {
		return strings.Compare(left, right)
	}
	for i := range l {
		if l[i] < r[i] {
			return -1
		}
		if l[i] > r[i] {
			return 1
		}
	}
	leftPre := strings.Contains(strings.TrimPrefix(left, "v"), "-")
	rightPre := strings.Contains(strings.TrimPrefix(right, "v"), "-")
	if leftPre != rightPre {
		if leftPre {
			return -1
		}
		return 1
	}
	if !leftPre {
		return 0
	}
	leftIdentifiers := strings.Split(strings.SplitN(strings.TrimPrefix(left, "v"), "-", 2)[1], ".")
	rightIdentifiers := strings.Split(strings.SplitN(strings.TrimPrefix(right, "v"), "-", 2)[1], ".")
	for index := 0; index < len(leftIdentifiers) && index < len(rightIdentifiers); index++ {
		leftNumber, leftErr := strconv.ParseUint(leftIdentifiers[index], 10, 64)
		rightNumber, rightErr := strconv.ParseUint(rightIdentifiers[index], 10, 64)
		switch {
		case leftErr == nil && rightErr == nil:
			if leftNumber < rightNumber {
				return -1
			}
			if leftNumber > rightNumber {
				return 1
			}
		case leftErr == nil:
			return -1
		case rightErr == nil:
			return 1
		default:
			if result := strings.Compare(leftIdentifiers[index], rightIdentifiers[index]); result != 0 {
				return result
			}
		}
	}
	if len(leftIdentifiers) < len(rightIdentifiers) {
		return -1
	}
	if len(leftIdentifiers) > len(rightIdentifiers) {
		return 1
	}
	return 0
}

func versionNumbers(value string) ([3]uint64, bool) {
	var result [3]uint64
	parsed, ok := parseVersion(value)
	if !ok {
		return result, false
	}
	parts := strings.Split(strings.SplitN(parsed, "-", 2)[0], ".")
	for i, part := range parts {
		result[i], _ = strconv.ParseUint(part, 10, 32)
	}
	return result, true
}

func validSHA256Digest(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validGitHubSHA256Digest(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(value, "sha256:") && validSHA256Digest(strings.TrimPrefix(value, "sha256:"))
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func readChecksum(path, expectedName string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	scanner := bufio.NewScanner(io.LimitReader(file, 64<<10))
	if !scanner.Scan() {
		return "", errors.New("校验文件为空")
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) != 2 || !validSHA256Digest(fields[0]) || strings.TrimPrefix(fields[1], "*") != expectedName {
		return "", errors.New("校验文件格式不正确")
	}
	if scanner.Scan() {
		return "", errors.New("校验文件包含多余内容")
	}
	return strings.ToLower(fields[0]), scanner.Err()
}

func extractPackage(zipPath, destination string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open update package: %w", err)
	}
	defer reader.Close()
	var total uint64
	seen := make(map[string]struct{}, len(reader.File))
	for _, entry := range reader.File {
		name := entry.Name
		if name == "" || filepath.Base(name) != name || strings.Contains(name, "..") || entry.FileInfo().IsDir() || entry.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("update package contains unsafe entry %q", name)
		}
		key := strings.ToLower(name)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("update package contains duplicate entry %q", name)
		}
		seen[key] = struct{}{}
		total += entry.UncompressedSize64
		if total > maxExtractedSize {
			return errors.New("update package expands beyond the size limit")
		}
		target := filepath.Join(destination, name)
		input, err := entry.Open()
		if err != nil {
			return err
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		var written int64
		if err == nil {
			written, err = io.Copy(output, io.LimitReader(input, int64(entry.UncompressedSize64)+1))
			if err == nil && uint64(written) != entry.UncompressedSize64 {
				err = errors.New("uncompressed size does not match ZIP metadata")
			}
		}
		closeInputErr := input.Close()
		if output != nil {
			if closeErr := output.Close(); err == nil {
				err = closeErr
			}
		}
		if err == nil {
			err = closeInputErr
		}
		if err != nil {
			return fmt.Errorf("extract %s: %w", name, err)
		}
	}
	return nil
}

func (s *Service) newPlan(stage, version string, request panel.PanelUpdateRequest) (Plan, error) {
	target, err := filepath.Abs(s.executable)
	if err != nil {
		return Plan{}, err
	}
	listenURL, err := healthURL(s.config.Listen)
	if err != nil {
		return Plan{}, err
	}
	return Plan{
		Version: version, PreviousVersion: buildinfo.Version, ParentPID: os.Getpid(),
		TaskName: updateTaskName, StageDir: stage, InstallDir: filepath.Dir(target),
		TargetExecutable: target, HealthURL: listenURL,
		ResultFile:        filepath.Join(s.config.Update.StagingDir, updateResultName),
		LogFile:           filepath.Join(s.config.Update.StagingDir, updateLogName),
		ActorCredentialID: request.ActorCredentialID, ActorRole: request.ActorRole, ActorIP: request.ActorIP,
	}, nil
}

func healthURL(listen string) (string, error) {
	parsed, err := url.Parse("http://" + listen)
	if err != nil {
		return "", err
	}
	host := parsed.Hostname()
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return "", errors.New("面板更新要求 Hearth 监听本机回环地址")
	}
	return "http://" + listen + "/api/v1/health", nil
}

func validateHealthURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "http" || parsed.Path != "/api/v1/health" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("health URL must be an HTTP loopback /api/v1/health endpoint")
	}
	host := parsed.Hostname()
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return errors.New("health URL must use a loopback host")
	}
	return nil
}

func writeJSONAtomic(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".hearth-update-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err == nil {
		_, err = temporary.Write(data)
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(temporaryPath, path)
}
