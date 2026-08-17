package dst

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"hearth/internal/panel"
)

const maxClusterTokenBytes = 4096

// ClusterTokenConfigured reports only whether the DST token file exists. The
// token itself is never loaded into Hearth configuration or returned to the UI.
func (s *Service) ClusterTokenConfigured() bool {
	return dstClusterTokenPresent(s.config.ClusterDir)
}

// UpdateClusterToken replaces cluster_token.txt while DST is stopped. The
// token is deliberately kept outside Hearth's configuration and audit data.
func (s *Service) UpdateClusterToken(token string) error {
	token = strings.TrimSpace(token)
	if token == "" || len([]byte(token)) > maxClusterTokenBytes ||
		!utf8.ValidString(token) || strings.ContainsAny(token, "\r\n\x00") {
		return fmt.Errorf("%w: DST Token 不能为空，且不能包含换行或非法字符", panel.ErrInvalid)
	}

	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return panel.ErrBusy
	}
	masterRunning, cavesRunning := s.masterRunning, s.cavesRunning
	executable, clusterDir := s.config.Executable, s.config.ClusterDir
	if masterRunning || cavesRunning {
		s.mu.Unlock()
		return fmt.Errorf("%w: DST 正在运行，请先停止 Master/Caves 后再更新 Token", panel.ErrUnsafe)
	}
	if processRunning(executable) {
		s.mu.Unlock()
		return fmt.Errorf("%w: 检测到外部 DST 进程，请先停止后再更新 Token", panel.ErrUnsafe)
	}
	s.busy = true
	s.currentAction = "token"
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.busy = false
		s.currentAction = ""
		s.mu.Unlock()
	}()

	if err := replaceClusterToken(filepath.Join(clusterDir, "cluster_token.txt"), []byte(token)); err != nil {
		return fmt.Errorf("write DST cluster token: %w", err)
	}
	return nil
}

func replaceClusterToken(path string, data []byte) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".hearth-cluster-token-*.tmp")
	if err != nil {
		return err
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
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}

	if runtime.GOOS != "windows" {
		if err := os.Rename(temporaryPath, path); err != nil {
			return err
		}
		cleanup = false
		return nil
	}

	// Windows does not replace an existing file with Rename. Keep the old
	// token until the new file is activated, and restore it if activation fails.
	backupPath := path + ".hearth-previous"
	_, statErr := os.Stat(path)
	hadPrevious := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	if hadPrevious {
		_ = os.Remove(backupPath)
		if err := os.Rename(path, backupPath); err != nil {
			return err
		}
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		if hadPrevious {
			_ = os.Rename(backupPath, path)
		}
		return err
	}
	if hadPrevious {
		_ = os.Remove(backupPath)
	}
	cleanup = false
	return nil
}
