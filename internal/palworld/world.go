package palworld

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"hearth/internal/panel"
)

const maxWorldOptionSize = 2 << 20

var worldIDPattern = regexp.MustCompile(`^[0-9A-Fa-f]{32}$`)

type activeWorld struct {
	ID         string
	Directory  string
	OptionPath string
	Detection  string
}

func detectActiveWorld(installDir string) (activeWorld, error) {
	saveRoot := filepath.Join(installDir, "Pal", "Saved", "SaveGames", "0")
	configuredID := readDedicatedServerName(filepath.Join(
		installDir, "Pal", "Saved", "Config", "WindowsServer", "GameUserSettings.ini",
	))
	if configuredID != "" {
		if world, ok := inspectWorld(saveRoot, configuredID, "GameUserSettings.ini"); ok {
			return world, nil
		}
	}

	entries, err := os.ReadDir(saveRoot)
	if err != nil {
		return activeWorld{}, fmt.Errorf("read Palworld save root: %w", err)
	}
	candidates := make([]activeWorld, 0, 1)
	for _, entry := range entries {
		if !entry.IsDir() || !worldIDPattern.MatchString(entry.Name()) {
			continue
		}
		if world, ok := inspectWorld(saveRoot, entry.Name(), "唯一存档目录"); ok {
			candidates = append(candidates, world)
		}
	}
	switch len(candidates) {
	case 0:
		return activeWorld{}, fmt.Errorf("%w: SaveGames\\0 下没有包含 Level.sav 的世界目录", panel.ErrNotFound)
	case 1:
		return candidates[0], nil
	default:
		return activeWorld{}, fmt.Errorf(
			"%w: 检测到 %d 个世界目录且 GameUserSettings.ini 未明确指定，已拒绝猜测",
			panel.ErrUnsafe, len(candidates),
		)
	}
}

func inspectWorld(root, id, detection string) (activeWorld, bool) {
	if !worldIDPattern.MatchString(id) {
		return activeWorld{}, false
	}
	directory := filepath.Join(root, strings.ToUpper(id))
	if _, err := os.Stat(filepath.Join(directory, "Level.sav")); err != nil {
		directory = filepath.Join(root, id)
		if _, secondErr := os.Stat(filepath.Join(directory, "Level.sav")); secondErr != nil {
			return activeWorld{}, false
		}
	}
	return activeWorld{
		ID: strings.ToUpper(filepath.Base(directory)), Directory: directory,
		OptionPath: filepath.Join(directory, "WorldOption.sav"), Detection: detection,
	}, true
}

func readDedicatedServerName(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		key, value, ok := strings.Cut(line, "=")
		if ok && strings.EqualFold(strings.TrimSpace(key), "DedicatedServerName") {
			return strings.Trim(strings.TrimSpace(value), `"`)
		}
	}
	return ""
}

func readWorldOption(world activeWorld) (panel.WorldOptionDocument, error) {
	file, err := os.Open(world.OptionPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return panel.WorldOptionDocument{}, fmt.Errorf(
				"%w: 当前世界 %s 没有 WorldOption.sav", panel.ErrNotFound, world.ID,
			)
		}
		return panel.WorldOptionDocument{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return panel.WorldOptionDocument{}, err
	}
	if info.Size() < 12 || info.Size() > maxWorldOptionSize {
		return panel.WorldOptionDocument{}, fmt.Errorf("%w: WorldOption.sav 文件大小异常", panel.ErrInvalid)
	}
	data, err := os.ReadFile(world.OptionPath)
	if err != nil {
		return panel.WorldOptionDocument{}, err
	}
	if err := validateWorldOptionContainer(data); err != nil {
		return panel.WorldOptionDocument{}, err
	}
	return panel.WorldOptionDocument{
		WorldID: world.ID, Revision: worldRevision(info), LastModified: info.ModTime(), Data: data,
	}, nil
}

func validateWorldOptionContainer(data []byte) error {
	if len(data) < 12 || len(data) > maxWorldOptionSize {
		return fmt.Errorf("%w: WorldOption.sav 文件大小异常", panel.ErrInvalid)
	}
	compressedLength := int(binary.LittleEndian.Uint32(data[4:8]))
	if compressedLength != len(data)-12 {
		return fmt.Errorf("%w: WorldOption.sav 压缩长度不匹配", panel.ErrInvalid)
	}
	if data[8] != 'P' || data[9] != 'l' || data[10] != 'Z' || (data[11] != '1' && data[11] != '2') {
		return fmt.Errorf("%w: WorldOption.sav 文件头无效", panel.ErrInvalid)
	}
	return nil
}

func worldRevision(info os.FileInfo) string {
	return fmt.Sprintf("%d-%d", info.ModTime().UnixNano(), info.Size())
}

func worldOptionModified(path, revision string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return worldRevision(info) != revision, nil
}
