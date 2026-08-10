package appupdate

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hearth/internal/buildinfo"
	"hearth/internal/config"
	"hearth/internal/panel"
)

func TestServiceChecksStagesAndLaunchesVerifiedRelease(t *testing.T) {
	previousVersion := buildinfo.Version
	buildinfo.Version = "1.1.0"
	t.Cleanup(func() { buildinfo.Version = previousVersion })

	packageData := testPackage(t, "1.2.0")
	digestBytes := sha256.Sum256(packageData)
	digest := hex.EncodeToString(digestBytes[:])
	zipName := "hearth-windows-amd64-v1.2.0.zip"
	checksum := []byte(digest + "  " + zipName + "\n")
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authorization := r.Header.Get("Authorization"); authorization != "" {
			t.Fatalf("anonymous release request sent authorization header: %q", authorization)
		}
		switch r.URL.Path {
		case "/repos/nikumatane/Hearth/releases/latest":
			_ = json.NewEncoder(w).Encode(githubRelease{TagName: "v1.2.0", Assets: []githubAsset{
				{Name: zipName, URL: server.URL + "/assets/package", Size: int64(len(packageData)), Digest: "sha256:" + digest},
				{Name: zipName + ".sha256", URL: server.URL + "/assets/checksum", Size: int64(len(checksum))},
			}})
		case "/assets/package":
			if r.Header.Get("Accept") != "application/octet-stream" {
				t.Errorf("package Accept = %q", r.Header.Get("Accept"))
			}
			_, _ = w.Write(packageData)
		case "/assets/checksum":
			_, _ = w.Write(checksum)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	temporary := t.TempDir()
	installed := filepath.Join(temporary, "installed", "hearth.exe")
	if err := os.MkdirAll(filepath.Dir(installed), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installed, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	launched := make(chan string, 1)
	service, err := New(config.Config{
		Listen: "127.0.0.1:18080",
		Update: config.UpdateConfig{Channel: "stable", StagingDir: filepath.Join(temporary, "updates")},
	}, filepath.Join(temporary, "config.json"), Options{
		APIBase: server.URL, Executable: installed, RuntimeGOOS: "windows",
		Launch: func(path string) error { launched <- path; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	status, err := service.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.UpdateAvailable || status.LatestVersion != "1.2.0" {
		t.Fatalf("unexpected status: %+v", status)
	}
	if _, err := service.ApplyUpdate(panel.PanelUpdateRequest{Version: "1.2.0", Confirm: true, ActorCredentialID: "ADMIN", ActorRole: "admin", ActorIP: "127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	helper := <-launched
	if filepath.Base(helper) != "hearth-updater.exe" {
		t.Fatalf("launched %s", helper)
	}
	plan, err := ReadPlan(filepath.Join(filepath.Dir(helper), updatePlanName))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Version != "1.2.0" || plan.TargetExecutable != installed {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}

func TestPrereleaseCheckSelectsHighestSemanticVersionRegardlessOfAPIOrder(t *testing.T) {
	previousVersion := buildinfo.Version
	buildinfo.Version = "1.2.0-rc.9"
	t.Cleanup(func() { buildinfo.Version = previousVersion })

	packageData := testPackage(t, "1.2.0-rc.10")
	digestBytes := sha256.Sum256(packageData)
	digest := hex.EncodeToString(digestBytes[:])
	zipName := "hearth-windows-amd64-v1.2.0-rc.10.zip"
	checksumSize := int64(len(digest + "  " + zipName + "\n"))
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/nikumatane/Hearth/releases" || r.URL.Query().Get("per_page") != "100" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode([]githubRelease{
			{TagName: "v9.9.9", Draft: true},
			{TagName: "v1.2.0-rc.9", Prerelease: true},
			{TagName: "v1.2.0-rc.10", Prerelease: true, Assets: []githubAsset{
				{Name: zipName, URL: server.URL + "/assets/package", Size: int64(len(packageData)), Digest: "sha256:" + digest},
				{Name: zipName + ".sha256", URL: server.URL + "/assets/checksum", Size: checksumSize},
			}},
			{TagName: "nightly", Prerelease: true},
			{TagName: "v1.2.0-rc.8", Prerelease: true},
		})
	}))
	defer server.Close()

	service, err := New(
		config.Config{Listen: "127.0.0.1:8080", Update: config.UpdateConfig{Channel: "prerelease", StagingDir: t.TempDir()}},
		"",
		Options{APIBase: server.URL, Executable: filepath.Join(t.TempDir(), "hearth.exe")},
	)
	if err != nil {
		t.Fatal(err)
	}
	status, err := service.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.UpdateAvailable || status.LatestVersion != "1.2.0-rc.10" {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestServiceRejectsReleaseWithoutGitHubDigest(t *testing.T) {
	previousVersion := buildinfo.Version
	buildinfo.Version = "1.1.0"
	t.Cleanup(func() { buildinfo.Version = previousVersion })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(githubRelease{TagName: "v1.2.0", Assets: []githubAsset{
			{Name: "hearth-windows-amd64-v1.2.0.zip", URL: "https://example.invalid/package", Size: 1},
			{Name: "hearth-windows-amd64-v1.2.0.zip.sha256", URL: "https://example.invalid/checksum", Size: 1},
		}})
	}))
	defer server.Close()
	service, err := New(config.Config{Listen: "127.0.0.1:8080", Update: config.UpdateConfig{Channel: "stable", StagingDir: t.TempDir()}}, "", Options{APIBase: server.URL, Executable: filepath.Join(t.TempDir(), "hearth.exe")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CheckForUpdate(context.Background()); err == nil {
		t.Fatal("expected missing digest to be rejected")
	}
}

func TestParseVersion(t *testing.T) {
	for _, value := range []string{"v1.2.0", "1.2.0-rc.1"} {
		if _, ok := parseVersion(value); !ok {
			t.Errorf("expected %s to be valid", value)
		}
	}
	for _, value := range []string{"1.2", "1.02.0", "1.2.0/evil", "1.2.0-../evil", "latest"} {
		if _, ok := parseVersion(value); ok {
			t.Errorf("expected %s to be invalid", value)
		}
	}
}

func TestCompareVersionsUsesSemanticPrereleaseOrder(t *testing.T) {
	for _, test := range []struct {
		left, right string
		expected    int
	}{
		{"1.2.0", "1.1.9", 1}, {"1.2.0-rc.2", "1.2.0-rc.10", -1},
		{"1.2.0", "1.2.0-rc.10", 1}, {"1.2.0-rc.1", "1.2.0-rc.1", 0},
	} {
		actual := compareVersions(test.left, test.right)
		if (actual < 0 && test.expected >= 0) || (actual > 0 && test.expected <= 0) || (actual == 0 && test.expected != 0) {
			t.Errorf("compareVersions(%q, %q) = %d, expected sign %d", test.left, test.right, actual, test.expected)
		}
	}
}

func TestSafeRequestErrorDoesNotExposeSignedURL(t *testing.T) {
	err := safeRequestError("download package", &url.Error{Op: "Get", URL: "https://example.invalid/file?jwt=secret", Err: context.DeadlineExceeded})
	if strings.Contains(err.Error(), "jwt=") || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("unsafe request error: %s", err)
	}
}

func TestDefaultHTTPClientsSeparateMetadataAndDownloadTimeouts(t *testing.T) {
	service, err := New(
		config.Config{Update: config.UpdateConfig{Channel: "stable"}},
		"",
		Options{Executable: filepath.Join(t.TempDir(), "hearth.exe")},
	)
	if err != nil {
		t.Fatal(err)
	}
	if service.client == service.downloadClient {
		t.Fatal("metadata and asset downloads share one HTTP client")
	}
	if service.client.Timeout != metadataTimeout || service.downloadClient.Timeout != downloadTimeout {
		t.Fatalf("timeouts = %s, %s", service.client.Timeout, service.downloadClient.Timeout)
	}
	for name, client := range map[string]*http.Client{"metadata": service.client, "download": service.downloadClient} {
		transport, ok := client.Transport.(*http.Transport)
		if !ok || transport.ResponseHeaderTimeout != 30*time.Second {
			t.Fatalf("%s response header timeout is not bounded", name)
		}
	}
}

func TestDownloadAssetRemovesIncompleteTemporaryFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("short"))
	}))
	defer server.Close()
	service := &Service{downloadClient: server.Client()}
	path := filepath.Join(t.TempDir(), "package.zip")
	err := service.downloadAsset(context.Background(), releaseAsset{Name: "package.zip", URL: server.URL, Size: 10}, path, 20)
	if err == nil {
		t.Fatal("downloadAsset() accepted a truncated response")
	}
	for _, candidate := range []string{path, path + ".part"} {
		if _, statErr := os.Stat(candidate); !os.IsNotExist(statErr) {
			t.Fatalf("incomplete download remains at %s: %v", candidate, statErr)
		}
	}
}

func TestDownloadAssetReportsMonotonicByteProgress(t *testing.T) {
	payload := bytes.Repeat([]byte("progress"), 16<<10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	service := &Service{
		downloadClient: server.Client(),
		status:         panel.PanelUpdateStatus{State: "preparing", Stage: "下载更新包", Progress: 15},
	}
	var previous int64
	path := filepath.Join(t.TempDir(), "package.zip")
	err := service.downloadAssetWithProgress(
		context.Background(),
		releaseAsset{Name: "package.zip", URL: server.URL, Size: int64(len(payload))},
		path,
		int64(len(payload)),
		func(written, total int64) {
			if written < previous || written > total {
				t.Errorf("progress = %d/%d after %d", written, total, previous)
			}
			previous = written
			service.setDownloadProgress(written, total)
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if previous != int64(len(payload)) {
		t.Fatalf("last progress = %d; want %d", previous, len(payload))
	}
	status := service.UpdateStatus()
	if status.Progress != 40 || !strings.Contains(status.Message, "MiB") {
		t.Fatalf("status = %+v", status)
	}
}

func TestUpdateStatusImportsResultWrittenAfterServiceStart(t *testing.T) {
	previousVersion := buildinfo.Version
	buildinfo.Version = "1.2.0-rc.9"
	t.Cleanup(func() { buildinfo.Version = previousVersion })

	stagingDir := t.TempDir()
	service, err := New(
		config.Config{Update: config.UpdateConfig{Channel: "prerelease", StagingDir: stagingDir}},
		"",
		Options{Executable: filepath.Join(t.TempDir(), "hearth.exe"), RuntimeGOOS: "windows"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if status := service.UpdateStatus(); status.State != "idle" {
		t.Fatalf("initial status = %+v", status)
	}

	completedAt := time.Date(2026, 8, 4, 19, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	result := Result{
		State: "succeeded", Stage: "面板更新完成", Version: buildinfo.Version, PreviousVersion: "1.2.0-rc.8",
		Message: "新版本已通过健康检查", CompletedAt: completedAt,
		ActorCredentialID: "ADMIN", ActorRole: "admin", ActorIP: "127.0.0.1",
	}
	if err := writeJSONAtomic(filepath.Join(stagingDir, updateResultName), &result); err != nil {
		t.Fatal(err)
	}

	status := service.UpdateStatus()
	if status.State != "succeeded" || status.Stage != result.Stage || status.Progress != 100 || status.LatestVersion != buildinfo.Version {
		t.Fatalf("imported status = %+v", status)
	}
	claimed := service.ConsumeUpdateResult()
	if claimed == nil || claimed.Version != buildinfo.Version {
		t.Fatalf("claimed result = %+v", claimed)
	}
	if err := service.CompleteUpdateResultImport(true); err != nil {
		t.Fatal(err)
	}
	persisted, err := service.readResult()
	if err != nil {
		t.Fatal(err)
	}
	if persisted == nil || !persisted.Consumed {
		t.Fatalf("persisted result = %+v", persisted)
	}
	if duplicate := service.ConsumeUpdateResult(); duplicate != nil {
		t.Fatalf("result imported twice: %+v", duplicate)
	}
}

func TestServiceIgnoresTerminalResultForAnotherRuntimeVersion(t *testing.T) {
	previousVersion := buildinfo.Version
	buildinfo.Version = "1.2.0-rc.9"
	t.Cleanup(func() { buildinfo.Version = previousVersion })

	stagingDir := t.TempDir()
	result := Result{
		State: "succeeded", Stage: "面板更新完成", Version: "1.2.0-rc.8", PreviousVersion: "1.2.0-rc.7",
		Message: "stale result", CompletedAt: time.Now(),
		ActorCredentialID: "ADMIN", ActorRole: "admin", ActorIP: "127.0.0.1", Consumed: true,
	}
	if err := writeJSONAtomic(filepath.Join(stagingDir, updateResultName), &result); err != nil {
		t.Fatal(err)
	}
	service, err := New(
		config.Config{Update: config.UpdateConfig{Channel: "prerelease", StagingDir: stagingDir}},
		"",
		Options{Executable: filepath.Join(t.TempDir(), "hearth.exe"), RuntimeGOOS: "windows"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if status := service.UpdateStatus(); status.State != "idle" || status.Stage != "等待检查" {
		t.Fatalf("stale result changed status: %+v", status)
	}
}

func TestServiceIgnoresConsumedTerminalResultForCurrentVersion(t *testing.T) {
	previousVersion := buildinfo.Version
	buildinfo.Version = "1.2.0-rc.9"
	t.Cleanup(func() { buildinfo.Version = previousVersion })

	stagingDir := t.TempDir()
	result := Result{
		State: "failed", Stage: "旧更新失败", Version: buildinfo.Version, PreviousVersion: "1.2.0-rc.8",
		Message: "consumed failure from an earlier attempt", CompletedAt: time.Now(),
		ActorCredentialID: "ADMIN", ActorRole: "admin", ActorIP: "127.0.0.1", Consumed: true,
	}
	if err := writeJSONAtomic(filepath.Join(stagingDir, updateResultName), &result); err != nil {
		t.Fatal(err)
	}
	service, err := New(
		config.Config{Update: config.UpdateConfig{Channel: "prerelease", StagingDir: stagingDir}},
		"",
		Options{Executable: filepath.Join(t.TempDir(), "hearth.exe"), RuntimeGOOS: "windows"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if status := service.UpdateStatus(); status.State != "idle" || status.Stage != "等待检查" {
		t.Fatalf("consumed result changed status: %+v", status)
	}
	if duplicate := service.ConsumeUpdateResult(); duplicate != nil {
		t.Fatalf("consumed result was imported again: %+v", duplicate)
	}
}

func testPackage(t *testing.T, version string) []byte {
	t.Helper()
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	for name, content := range map[string]string{"hearth.exe": "new-panel", "hearth-updater.exe": "new-updater", "VERSION": version} {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprint(entry, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
