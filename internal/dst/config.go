package dst

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"hearth/internal/panel"
)

const maxDSTConfigFileSize = 512 << 10

type dstConfigFileDefinition struct {
	id       string
	name     string
	format   string
	required bool
	path     func(string) string
}

var dstConfigFiles = []dstConfigFileDefinition{
	{id: "cluster", name: "cluster.ini", format: "ini", required: true, path: func(dir string) string { return filepath.Join(dir, "cluster.ini") }},
	{id: "master", name: "Master/server.ini", format: "ini", required: true, path: func(dir string) string { return filepath.Join(dir, "Master", "server.ini") }},
	{id: "caves", name: "Caves/server.ini", format: "ini", required: true, path: func(dir string) string { return filepath.Join(dir, "Caves", "server.ini") }},
	{id: "master-world", name: "Master/worldgenoverride.lua", format: "worldgen", path: func(dir string) string { return filepath.Join(dir, "Master", "worldgenoverride.lua") }},
	{id: "caves-world", name: "Caves/worldgenoverride.lua", format: "worldgen", path: func(dir string) string { return filepath.Join(dir, "Caves", "worldgenoverride.lua") }},
}

func (s *Service) DSTConfig() (panel.DSTConfigDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readDSTConfigLocked()
}

func (s *Service) UpdateDSTConfig(patch panel.DSTConfigPatch) (panel.DSTConfigDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureDSTConfigWritableLocked(); err != nil {
		return panel.DSTConfigDocument{}, err
	}
	current, err := s.readDSTConfigLocked()
	if err != nil {
		return panel.DSTConfigDocument{}, err
	}
	if strings.TrimSpace(patch.Revision) == "" || patch.Revision != current.Revision {
		return panel.DSTConfigDocument{}, fmt.Errorf("%w: DST 配置已变化，请重新读取后再保存", panel.ErrInvalid)
	}
	if len(patch.Files) == 0 {
		return panel.DSTConfigDocument{}, fmt.Errorf("%w: 至少需要提交一个 DST 配置文件", panel.ErrInvalid)
	}
	return s.writeDSTConfigLocked(current, patch.Files)
}

func (s *Service) ensureDSTConfigWritableLocked() error {
	if s.busy || s.masterRunning || s.cavesRunning || processRunning(s.config.ProcessName) {
		return fmt.Errorf("%w: DST 运行中或有任务正在执行", panel.ErrUnsafe)
	}
	return nil
}

func (s *Service) writeDSTConfigLocked(current panel.DSTConfigDocument, files map[string]string) (panel.DSTConfigDocument, error) {
	allowed := make(map[string]dstConfigFileDefinition, len(dstConfigFiles))
	oldContent := make(map[string]string, len(current.Files))
	oldExists := make(map[string]bool, len(current.Files))
	for _, definition := range dstConfigFiles {
		allowed[definition.id] = definition
	}
	for _, file := range current.Files {
		oldContent[file.ID] = file.Content
		oldExists[file.ID] = file.Exists
	}
	ids := make([]string, 0, len(files))
	for id, content := range files {
		definition, ok := allowed[id]
		if !ok {
			return panel.DSTConfigDocument{}, fmt.Errorf("%w: 不支持的 DST 配置文件 %q", panel.ErrInvalid, id)
		}
		if len(content) > maxDSTConfigFileSize {
			return panel.DSTConfigDocument{}, fmt.Errorf("%w: %s 超过 512 KiB", panel.ErrInvalid, definition.name)
		}
		if strings.ContainsRune(content, '\x00') || !utf8.ValidString(content) {
			return panel.DSTConfigDocument{}, fmt.Errorf("%w: %s 包含无效字符", panel.ErrInvalid, definition.name)
		}
		if strings.TrimSpace(content) == "" {
			return panel.DSTConfigDocument{}, fmt.Errorf("%w: %s 不能为空", panel.ErrInvalid, definition.name)
		}
		var validationErr error
		if definition.format == "worldgen" {
			_, validationErr = parseDSTWorldgen(content)
		} else {
			validationErr = validateDSTINI(content)
		}
		if validationErr != nil {
			return panel.DSTConfigDocument{}, fmt.Errorf("%w: %s 格式无效: %v", panel.ErrInvalid, definition.name, validationErr)
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	written := make([]string, 0, len(ids))
	for _, id := range ids {
		if err := replaceClusterToken(dstConfigPath(s.config.ClusterDir, id), []byte(files[id])); err != nil {
			for _, rollbackID := range written {
				if oldExists[rollbackID] {
					_ = replaceClusterToken(dstConfigPath(s.config.ClusterDir, rollbackID), []byte(oldContent[rollbackID]))
				} else {
					_ = os.Remove(dstConfigPath(s.config.ClusterDir, rollbackID))
				}
			}
			return panel.DSTConfigDocument{}, fmt.Errorf("写入 DST %s: %w", allowed[id].name, err)
		}
		written = append(written, id)
	}
	return s.readDSTConfigLocked()
}

func validateDSTINI(content string) error {
	for lineNumber, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if lineNumber == 0 {
			line = strings.TrimPrefix(line, "\uFEFF")
		}
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") || len(strings.TrimSpace(line[1:len(line)-1])) == 0 {
				return fmt.Errorf("第 %d 行 section 无效", lineNumber+1)
			}
			continue
		}
		equal := strings.IndexByte(line, '=')
		if equal <= 0 || strings.TrimSpace(line[:equal]) == "" {
			return fmt.Errorf("第 %d 行必须是 key=value", lineNumber+1)
		}
	}
	return nil
}

func (s *Service) readDSTConfigLocked() (panel.DSTConfigDocument, error) {
	if strings.TrimSpace(s.config.ClusterDir) == "" {
		return panel.DSTConfigDocument{}, panel.ErrNotFound
	}
	files := make([]panel.DSTConfigFile, 0, len(dstConfigFiles))
	hash := sha256.New()
	var lastModified time.Time
	for _, definition := range dstConfigFiles {
		path := definition.path(s.config.ClusterDir)
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) && !definition.required {
			data := []byte(defaultDSTWorldgen(definition.id))
			fileRevision := sha256.Sum256(data)
			_, _ = hash.Write([]byte(definition.id))
			_, _ = hash.Write([]byte{0, 0})
			files = append(files, panel.DSTConfigFile{
				ID: definition.id, Name: definition.name, Format: definition.format, Exists: false,
				Revision: hex.EncodeToString(fileRevision[:]), Content: string(data),
			})
			continue
		}
		if err != nil || info.IsDir() {
			if err == nil {
				err = errors.New("path is a directory")
			}
			return panel.DSTConfigDocument{}, fmt.Errorf("%w: DST %s 不可读取: %v", panel.ErrInvalid, definition.name, err)
		}
		if info.Size() > maxDSTConfigFileSize {
			return panel.DSTConfigDocument{}, fmt.Errorf("%w: DST %s 超过 512 KiB", panel.ErrInvalid, definition.name)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return panel.DSTConfigDocument{}, fmt.Errorf("读取 DST %s: %w", definition.name, err)
		}
		if !utf8.Valid(data) || strings.ContainsRune(string(data), '\x00') {
			return panel.DSTConfigDocument{}, fmt.Errorf("%w: DST %s 包含无效字符", panel.ErrInvalid, definition.name)
		}
		fileRevision := sha256.Sum256(data)
		modified := info.ModTime()
		if modified.After(lastModified) {
			lastModified = modified
		}
		_, _ = hash.Write([]byte(definition.id))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(data)
		files = append(files, panel.DSTConfigFile{
			ID: definition.id, Name: definition.name, Format: definition.format, Exists: true,
			Revision: hex.EncodeToString(fileRevision[:]), LastModified: modified, Content: string(data),
		})
	}
	return panel.DSTConfigDocument{Revision: hex.EncodeToString(hash.Sum(nil)), LastModified: lastModified, Files: files}, nil
}

func dstConfigPath(clusterDir, id string) string {
	for _, definition := range dstConfigFiles {
		if definition.id == id {
			return definition.path(clusterDir)
		}
	}
	return ""
}
