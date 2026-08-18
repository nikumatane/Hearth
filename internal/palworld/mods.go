package palworld

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	modmodel "hearth/internal/mods"
	"hearth/internal/panel"
)

const (
	maxPalworldWorkshopEntries = 256
	maxPalworldModInfoBytes    = 1 << 20
	maxPalworldModSettingsSize = 1 << 20
)

type palworldModInfo struct {
	PackageName  string
	Name         string
	Version      string
	InstallRules json.RawMessage
	Dependencies json.RawMessage
}

func (s *Service) ModInventory() (modmodel.Inventory, error) {
	return scanPalworldMods(s.config.InstallDir, time.Now())
}

func scanPalworldMods(installDir string, now time.Time) (modmodel.Inventory, error) {
	if strings.TrimSpace(installDir) == "" || !filepath.IsAbs(installDir) {
		return modmodel.Inventory{}, fmt.Errorf("%w: Palworld install directory is invalid", panel.ErrInvalid)
	}
	inventory := modmodel.Inventory{
		GameID: palworldID, Managed: true, Mods: []modmodel.Descriptor{}, Warnings: []string{}, ScannedAt: now,
	}
	hash := sha256.New()
	settingsPath := filepath.Join(installDir, "Mods", "PalModSettings.ini")
	settingsData, settingsErr := readBoundedModFile(settingsPath, maxPalworldModSettingsSize)
	globalEnabled, activePackages, externalRoot := false, map[string]string{}, ""
	switch {
	case settingsErr == nil:
		_, _ = hash.Write([]byte("settings\x00"))
		_, _ = hash.Write(settingsData)
		var parseErr error
		globalEnabled, activePackages, externalRoot, parseErr = parsePalModSettings(settingsData)
		if parseErr != nil {
			globalEnabled, activePackages = false, map[string]string{}
			inventory.Warnings = append(inventory.Warnings, "PalModSettings.ini 包含超长行或无法完整解析，启用状态按关闭处理")
		}
	case errors.Is(settingsErr, os.ErrNotExist):
		_, _ = hash.Write([]byte("settings-missing\x00"))
	default:
		inventory.Warnings = append(inventory.Warnings, "PalModSettings.ini 无法安全读取，启用状态按关闭处理")
		_, _ = hash.Write([]byte("settings-unreadable\x00"))
	}
	if externalRoot != "" {
		inventory.Warnings = append(inventory.Warnings, "检测到外部 WorkshopRootDir；当前只读清单仅扫描 PalServer 默认 Mods/Workshop 目录")
	}

	workshopRoot := filepath.Join(installDir, "Mods", "Workshop")
	entries, err := os.ReadDir(workshopRoot)
	if errors.Is(err, os.ErrNotExist) {
		inventory.Revision = fmt.Sprintf("%x", hash.Sum(nil))
		appendMissingActiveWarnings(&inventory, activePackages, nil)
		return inventory, nil
	}
	if err != nil {
		return modmodel.Inventory{}, fmt.Errorf("read Palworld Workshop directory: %w", err)
	}
	if len(entries) > maxPalworldWorkshopEntries {
		inventory.Warnings = append(inventory.Warnings, fmt.Sprintf("Workshop 目录超过 %d 个直接子项，仅扫描前 %d 个", maxPalworldWorkshopEntries, maxPalworldWorkshopEntries))
		entries = entries[:maxPalworldWorkshopEntries]
	}
	resolvedWorkshopRoot, err := filepath.EvalSymlinks(workshopRoot)
	if err != nil {
		return modmodel.Inventory{}, fmt.Errorf("resolve Palworld Workshop directory: %w", err)
	}
	seenPackages := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		entryLabel := safeModEntryLabel(entry.Name())
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			inventory.Warnings = append(inventory.Warnings, fmt.Sprintf("已跳过非普通模组目录 %s", entryLabel))
			continue
		}
		resolvedModDir, resolveErr := filepath.EvalSymlinks(filepath.Join(workshopRoot, entry.Name()))
		if resolveErr != nil || !pathWithinModRoot(resolvedWorkshopRoot, resolvedModDir) {
			inventory.Warnings = append(inventory.Warnings, fmt.Sprintf("%s 的真实路径不在默认 Workshop 目录内，已跳过", entryLabel))
			continue
		}
		infoPath := filepath.Join(workshopRoot, entry.Name(), "Info.json")
		data, readErr := readBoundedModFile(infoPath, maxPalworldModInfoBytes)
		if readErr != nil {
			if errors.Is(readErr, os.ErrNotExist) {
				inventory.Warnings = append(inventory.Warnings, fmt.Sprintf("%s 缺少 Info.json", entryLabel))
			} else {
				inventory.Warnings = append(inventory.Warnings, fmt.Sprintf("%s 的 Info.json 无法安全读取", entryLabel))
			}
			continue
		}
		_, _ = hash.Write([]byte(entry.Name()))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(data)
		info, parseErr := parsePalworldModInfo(data)
		if parseErr != nil {
			inventory.Warnings = append(inventory.Warnings, fmt.Sprintf("%s 的 Info.json 无效：%s", entryLabel, parseErr))
			continue
		}
		packageKey := strings.ToLower(info.PackageName)
		if _, duplicate := seenPackages[packageKey]; duplicate {
			inventory.Warnings = append(inventory.Warnings, fmt.Sprintf("%s 的 PackageName 与其他模组重复，已跳过", entryLabel))
			continue
		}
		seenPackages[packageKey] = struct{}{}
		compatibility, compatibilityWarning := palworldServerCompatibility(info.InstallRules)
		dependencies, dependencyWarning := palworldModDependencies(info.Dependencies)
		warnings := []string{}
		if compatibilityWarning != "" {
			warnings = append(warnings, compatibilityWarning)
		}
		if dependencyWarning != "" {
			warnings = append(warnings, dependencyWarning)
		}
		_, enabled := activePackages[packageKey]
		inventory.Mods = append(inventory.Mods, modmodel.Descriptor{
			ID: info.PackageName, GameID: palworldID, Name: info.Name,
			Source:          modmodel.SourceOfficialPackage,
			SourceReference: filepath.ToSlash(filepath.Join("Mods", "Workshop", entry.Name())),
			Version:         info.Version, Enabled: globalEnabled && enabled,
			Ownership: modmodel.OwnershipExternal, Compatibility: compatibility,
			Dependencies: dependencies, Warnings: warnings,
		})
	}
	sort.Slice(inventory.Mods, func(left, right int) bool {
		return strings.ToLower(inventory.Mods[left].ID) < strings.ToLower(inventory.Mods[right].ID)
	})
	appendMissingActiveWarnings(&inventory, activePackages, seenPackages)
	inventory.Revision = fmt.Sprintf("%x", hash.Sum(nil))
	return inventory, nil
}

func readBoundedModFile(path string, maxBytes int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxBytes {
		return nil, errors.New("file type or size is outside the safe boundary")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) || openedInfo.Size() <= 0 || openedInfo.Size() > maxBytes {
		return nil, errors.New("file changed or moved outside the safe boundary")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, errors.New("file grew outside the safe boundary")
	}
	return data, nil
}

func parsePalModSettings(data []byte) (bool, map[string]string, string, error) {
	active := make(map[string]string)
	section, workshopRoot := "", ""
	globalEnabled := false
	scanner := bufio.NewScanner(bytes.NewReader(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))))
	scanner.Buffer(make([]byte, 4096), 64<<10)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		if !strings.EqualFold(section, "PalModSettings") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch {
		case strings.EqualFold(key, "bGlobalEnableMod"):
			globalEnabled = strings.EqualFold(value, "true") || value == "1"
		case strings.EqualFold(key, "ActiveModList") && safePalworldPackageName(value):
			packageKey := strings.ToLower(value)
			if _, duplicate := active[packageKey]; !duplicate {
				active[packageKey] = value
			}
		case strings.EqualFold(key, "WorkshopRootDir"):
			workshopRoot = value
		}
	}
	return globalEnabled, active, workshopRoot, scanner.Err()
}

func parsePalworldModInfo(data []byte) (palworldModInfo, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var document map[string]json.RawMessage
	if err := decoder.Decode(&document); err != nil {
		return palworldModInfo{}, errors.New("JSON 无法解析")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return palworldModInfo{}, errors.New("JSON 后存在额外内容")
	}
	packageName, err := jsonStringField(document, "PackageName")
	if err != nil || !safePalworldPackageName(packageName) {
		return palworldModInfo{}, errors.New("PackageName 缺失或格式不安全")
	}
	name, _ := firstJSONTextField(document, "DisplayName", "Name", "FriendlyName")
	if !safeModText(name, 256) {
		name = packageName
	}
	version, _ := firstJSONTextField(document, "Version")
	if !safeModText(version, 128) {
		version = ""
	}
	installRules := rawJSONField(document, "InstallRules")
	if len(installRules) == 0 {
		installRules = rawJSONField(document, "InstallRule")
	}
	return palworldModInfo{
		PackageName: packageName, Name: name, Version: version,
		InstallRules: installRules, Dependencies: rawJSONField(document, "Dependencies"),
	}, nil
}

func jsonStringField(document map[string]json.RawMessage, name string) (string, error) {
	raw := rawJSONField(document, name)
	var value string
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return "", errors.New("field is not a string")
	}
	return strings.TrimSpace(value), nil
}

func firstJSONTextField(document map[string]json.RawMessage, names ...string) (string, error) {
	for _, name := range names {
		raw := rawJSONField(document, name)
		if len(raw) == 0 {
			continue
		}
		var text string
		if json.Unmarshal(raw, &text) == nil {
			return strings.TrimSpace(text), nil
		}
		var number json.Number
		if json.Unmarshal(raw, &number) == nil {
			return number.String(), nil
		}
	}
	return "", errors.New("field is unavailable")
}

func rawJSONField(document map[string]json.RawMessage, name string) json.RawMessage {
	for key, value := range document {
		if strings.EqualFold(key, name) {
			return value
		}
	}
	return nil
}

func palworldServerCompatibility(raw json.RawMessage) (modmodel.Compatibility, string) {
	if len(raw) == 0 {
		return modmodel.CompatibilityUnknown, "Info.json 未声明可识别的 InstallRules，无法确认服务端兼容性"
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return modmodel.CompatibilityUnknown, "InstallRules 格式无法识别，无法确认服务端兼容性"
	}
	seen, supported := findServerCompatibilityFlag(value)
	if supported {
		return modmodel.CompatibilitySupported, ""
	}
	if seen {
		return modmodel.CompatibilityUnsupported, "Info.json 的 InstallRules 未允许服务端部署"
	}
	return modmodel.CompatibilityUnknown, "InstallRules 未包含 IsServer 标记，无法确认服务端兼容性"
}

func findServerCompatibilityFlag(value any) (seen, supported bool) {
	switch typed := value.(type) {
	case []any:
		for _, child := range typed {
			childSeen, childSupported := findServerCompatibilityFlag(child)
			seen, supported = seen || childSeen, supported || childSupported
		}
	case map[string]any:
		for key, child := range typed {
			if strings.EqualFold(key, "IsServer") {
				if flag, ok := child.(bool); ok {
					seen, supported = true, supported || flag
				}
				continue
			}
			childSeen, childSupported := findServerCompatibilityFlag(child)
			seen, supported = seen || childSeen, supported || childSupported
		}
	}
	return seen, supported
}

func palworldModDependencies(raw json.RawMessage) ([]string, string) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return []string{}, ""
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return []string{}, "Dependencies 格式无法识别"
	}
	values := []string{}
	collectDependencyNames(value, &values)
	unique := make(map[string]string, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if safePalworldPackageName(value) {
			key := strings.ToLower(value)
			if _, duplicate := unique[key]; !duplicate {
				unique[key] = value
			}
		}
	}
	dependencies := make([]string, 0, len(unique))
	for _, value := range unique {
		dependencies = append(dependencies, value)
	}
	sort.Slice(dependencies, func(left, right int) bool {
		return strings.ToLower(dependencies[left]) < strings.ToLower(dependencies[right])
	})
	if len(dependencies) == 0 {
		return dependencies, "Dependencies 存在但没有可安全识别的 PackageName"
	}
	return dependencies, ""
}

func collectDependencyNames(value any, values *[]string) {
	if len(*values) >= 64 {
		return
	}
	switch typed := value.(type) {
	case string:
		*values = append(*values, typed)
	case []any:
		for _, child := range typed {
			collectDependencyNames(child, values)
		}
	case map[string]any:
		foundPackageName := false
		for key, child := range typed {
			if strings.EqualFold(key, "PackageName") {
				if name, ok := child.(string); ok {
					*values = append(*values, name)
					foundPackageName = true
				}
			}
		}
		if foundPackageName {
			return
		}
		for _, child := range typed {
			switch child.(type) {
			case []any, map[string]any:
				collectDependencyNames(child, values)
			}
		}
	}
}

func appendMissingActiveWarnings(inventory *modmodel.Inventory, active map[string]string, discovered map[string]struct{}) {
	for packageKey, packageName := range active {
		if _, exists := discovered[packageKey]; !exists {
			inventory.Warnings = append(inventory.Warnings, fmt.Sprintf("PalModSettings.ini 启用了未扫描到的模组 %s", strconv.QuoteToASCII(packageName)))
		}
	}
	sort.Strings(inventory.Warnings)
}

func safePalworldPackageName(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '_' || char == '-' || char == '.' {
			continue
		}
		return false
	}
	return true
}

func safeModText(value string, maxRunes int) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	count := 0
	for _, char := range value {
		if unicode.IsControl(char) {
			return false
		}
		count++
		if count > maxRunes {
			return false
		}
	}
	return true
}

func safeModEntryLabel(value string) string {
	runes := []rune(value)
	if len(runes) > 96 {
		value = string(runes[:96])
	}
	return strconv.QuoteToASCII(value)
}

func pathWithinModRoot(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
