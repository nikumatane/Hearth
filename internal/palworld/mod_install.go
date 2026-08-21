package palworld

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	modmodel "hearth/internal/mods"
	"hearth/internal/panel"
	"hearth/internal/steamworkshop"
)

const (
	maxPalworldModArchiveBytes = 256 << 20
	maxPalworldModExtractBytes = 1 << 30
	maxPalworldModArchiveFiles = 8192
)

func (s *Service) LookupWorkshopItem(ctx context.Context, reference string) (modmodel.WorkshopItem, error) {
	if s.workshop == nil {
		return modmodel.WorkshopItem{}, errors.New("Steam Workshop 查询服务不可用")
	}
	return s.workshop.Lookup(ctx, palworldWorkshopAppID, reference)
}

// InstallWorkshopPackage validates and atomically installs a complete official
// package while leaving PalModSettings.ini unchanged. The mod therefore remains
// disabled until a later, separately confirmed enable operation.
func (s *Service) InstallWorkshopPackage(ctx context.Context, request modmodel.PackageInstallRequest) (modmodel.Inventory, error) {
	if !request.Confirm {
		return modmodel.Inventory{}, fmt.Errorf("%w: 安装模组需要管理员明确确认", panel.ErrInvalid)
	}
	workshopID, err := steamworkshop.ParseReference(request.WorkshopID)
	if err != nil {
		return modmodel.Inventory{}, err
	}
	if request.Package == nil || !strings.EqualFold(filepath.Ext(strings.TrimSpace(request.FileName)), ".zip") {
		return modmodel.Inventory{}, fmt.Errorf("%w: 请选择完整 Workshop 模组 ZIP 包", panel.ErrInvalid)
	}
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return modmodel.Inventory{}, panel.ErrBusy
	}
	s.busy = true
	s.currentAction = "mod_install"
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.busy = false
		s.currentAction = ""
		s.mu.Unlock()
	}()
	process, _, err := s.platform.sample(s.config.ProcessName, s.config.InstallDir)
	if err != nil {
		return modmodel.Inventory{}, err
	}
	if process.Running {
		return modmodel.Inventory{}, fmt.Errorf("%w: 安装模组前请先停止 Palworld 服务器", panel.ErrUnsafe)
	}
	current, err := s.ModInventory()
	if err != nil {
		return modmodel.Inventory{}, err
	}
	target := filepath.Join(s.config.InstallDir, "Mods", "Workshop", workshopID)
	if _, err := os.Lstat(target); err == nil {
		return modmodel.Inventory{}, fmt.Errorf("%w: Workshop ID %s 已存在，当前版本不会覆盖现有模组", panel.ErrUnsafe, workshopID)
	} else if !errors.Is(err, os.ErrNotExist) {
		return modmodel.Inventory{}, err
	}

	stagingParent := filepath.Join(s.config.InstallDir, "Mods", ".hearth-staging")
	if err := os.MkdirAll(stagingParent, 0o700); err != nil {
		return modmodel.Inventory{}, fmt.Errorf("create mod staging directory: %w", err)
	}
	staging, err := os.MkdirTemp(stagingParent, "install-")
	if err != nil {
		return modmodel.Inventory{}, err
	}
	defer os.RemoveAll(staging)
	archivePath := filepath.Join(staging, "package.zip")
	if err := copyBoundedPackage(ctx, request.Package, archivePath); err != nil {
		return modmodel.Inventory{}, err
	}
	payload := filepath.Join(staging, "payload")
	if err := os.MkdirAll(payload, 0o700); err != nil {
		return modmodel.Inventory{}, err
	}
	if err := extractPalworldModArchive(ctx, archivePath, payload); err != nil {
		return modmodel.Inventory{}, err
	}
	packageRoot, err := locatePalworldPackageRoot(payload, workshopID)
	if err != nil {
		return modmodel.Inventory{}, err
	}
	infoData, err := readBoundedModFile(filepath.Join(packageRoot, "Info.json"), maxPalworldModInfoBytes)
	if err != nil {
		return modmodel.Inventory{}, fmt.Errorf("%w: 模组包根目录缺少有效 Info.json", panel.ErrInvalid)
	}
	info, err := parsePalworldModInfo(infoData)
	if err != nil {
		return modmodel.Inventory{}, fmt.Errorf("%w: Info.json 无效：%v", panel.ErrInvalid, err)
	}
	compatibility, warning := palworldServerCompatibility(info.InstallRules)
	if compatibility != modmodel.CompatibilitySupported {
		if warning == "" {
			warning = "Info.json 未明确允许服务端部署"
		}
		return modmodel.Inventory{}, fmt.Errorf("%w: %s", panel.ErrInvalid, warning)
	}
	for _, installed := range current.Mods {
		if strings.EqualFold(installed.ID, info.PackageName) {
			return modmodel.Inventory{}, fmt.Errorf("%w: PackageName %s 已安装", panel.ErrUnsafe, info.PackageName)
		}
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return modmodel.Inventory{}, err
	}
	if err := os.Rename(packageRoot, target); err != nil {
		return modmodel.Inventory{}, fmt.Errorf("activate mod package: %w", err)
	}
	committed := true
	defer func() {
		if !committed {
			_ = os.RemoveAll(target)
		}
	}()
	managed, _, managedWarning := readPalworldManagedMods(s.config.InstallDir)
	if managedWarning != "" {
		committed = false
		return modmodel.Inventory{}, fmt.Errorf("%w: %s；为避免覆盖接管记录，已回滚安装", panel.ErrUnsafe, managedWarning)
	}
	managed[workshopID] = palworldManagedMod{PackageName: info.PackageName, InstalledAt: time.Now().UTC()}
	if err := writePalworldManagedMods(s.config.InstallDir, managed); err != nil {
		committed = false
		return modmodel.Inventory{}, fmt.Errorf("record Hearth-managed mod: %w", err)
	}
	updated, err := s.ModInventory()
	if err != nil {
		committed = false
		delete(managed, workshopID)
		_ = writePalworldManagedMods(s.config.InstallDir, managed)
		return modmodel.Inventory{}, fmt.Errorf("verify installed mod: %w", err)
	}
	verified := false
	for _, installed := range updated.Mods {
		if strings.EqualFold(installed.ID, info.PackageName) && installed.Ownership == modmodel.OwnershipHearth {
			verified = true
			break
		}
	}
	if !verified {
		committed = false
		delete(managed, workshopID)
		_ = writePalworldManagedMods(s.config.InstallDir, managed)
		return modmodel.Inventory{}, errors.New("安装后的模组未通过往返清单校验")
	}
	return updated, nil
}

func copyBoundedPackage(ctx context.Context, source io.Reader, destination string) error {
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	reader := &contextReader{ctx: ctx, reader: io.LimitReader(source, maxPalworldModArchiveBytes+1)}
	written, copyErr := io.Copy(file, reader)
	closeErr := file.Close()
	if copyErr != nil {
		return fmt.Errorf("upload mod package: %w", copyErr)
	}
	if closeErr != nil {
		return closeErr
	}
	if written == 0 || written > maxPalworldModArchiveBytes {
		return fmt.Errorf("%w: 模组 ZIP 必须在 1 B 到 256 MiB 之间", panel.ErrInvalid)
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.reader.Read(buffer)
	}
}

func extractPalworldModArchive(ctx context.Context, archivePath, destinationRoot string) error {
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("%w: 无法打开模组 ZIP", panel.ErrInvalid)
	}
	defer archive.Close()
	if len(archive.File) == 0 || len(archive.File) > maxPalworldModArchiveFiles {
		return fmt.Errorf("%w: 模组 ZIP 文件数量超出安全限制", panel.ErrInvalid)
	}
	seen := make(map[string]struct{}, len(archive.File))
	var extracted int64
	for _, entry := range archive.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := strings.ReplaceAll(entry.Name, "\\", "/")
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(name)))
		if clean == "." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || strings.Contains(clean, ":") {
			return fmt.Errorf("%w: 模组 ZIP 包含不安全路径", panel.ErrInvalid)
		}
		key := strings.ToLower(clean)
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("%w: 模组 ZIP 包含重复路径", panel.ErrInvalid)
		}
		seen[key] = struct{}{}
		if entry.Mode()&os.ModeSymlink != 0 || (!entry.FileInfo().IsDir() && !entry.Mode().IsRegular()) {
			return fmt.Errorf("%w: 模组 ZIP 包含不支持的文件类型", panel.ErrInvalid)
		}
		if entry.UncompressedSize64 > uint64(maxPalworldModExtractBytes) ||
			extracted > maxPalworldModExtractBytes-int64(entry.UncompressedSize64) {
			return fmt.Errorf("%w: 模组解压大小超过 1 GiB 安全限制", panel.ErrInvalid)
		}
		extracted += int64(entry.UncompressedSize64)
		destination := filepath.Join(destinationRoot, filepath.FromSlash(clean))
		if !pathWithinModRoot(destinationRoot, destination) {
			return fmt.Errorf("%w: 模组 ZIP 包含越界路径", panel.ErrInvalid)
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
		input, err := entry.Open()
		if err != nil {
			return err
		}
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			_ = input.Close()
			return err
		}
		written, copyErr := io.Copy(output, &contextReader{ctx: ctx, reader: io.LimitReader(input, int64(entry.UncompressedSize64)+1)})
		inputErr, outputErr := input.Close(), output.Close()
		if copyErr != nil || inputErr != nil || outputErr != nil || written != int64(entry.UncompressedSize64) {
			return fmt.Errorf("%w: 模组 ZIP 文件解压不完整", panel.ErrInvalid)
		}
	}
	return nil
}

func locatePalworldPackageRoot(payload, workshopID string) (string, error) {
	if _, err := os.Lstat(filepath.Join(payload, "Info.json")); err == nil {
		return payload, nil
	}
	entries, err := os.ReadDir(payload)
	if err != nil {
		return "", err
	}
	candidates := []string{}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		candidate := filepath.Join(payload, entry.Name())
		if _, err := os.Lstat(filepath.Join(candidate, "Info.json")); err == nil {
			if safeWorkshopID(entry.Name()) && entry.Name() != workshopID {
				return "", fmt.Errorf("%w: ZIP 目录 ID %s 与已确认的 %s 不一致", panel.ErrInvalid, entry.Name(), workshopID)
			}
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) != 1 {
		return "", fmt.Errorf("%w: ZIP 必须在根目录或唯一的直接子目录包含 Info.json", panel.ErrInvalid)
	}
	return candidates[0], nil
}
