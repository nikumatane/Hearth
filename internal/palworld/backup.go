package palworld

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const backupTimestampLayout = "20060102-150405.000000000"

type backupRetentionResult struct {
	Removed    int
	FreedBytes int64
}

type managedBackup struct {
	path      string
	createdAt time.Time
	size      int64
}

func createBackup(installDir, settingsFile, backupDir string) (string, error) {
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return "", fmt.Errorf("create backup directory: %w", err)
	}
	finalPath := filepath.Join(backupDir, "palworld-"+time.Now().Format(backupTimestampLayout)+".zip")
	temp, err := os.CreateTemp(backupDir, ".palworld-backup-*.tmp")
	if err != nil {
		return "", err
	}
	tempPath := temp.Name()
	ok := false
	defer func() {
		_ = temp.Close()
		if !ok {
			_ = os.Remove(tempPath)
		}
	}()

	archive := zip.NewWriter(temp)
	saveRoot := filepath.Join(installDir, "Pal", "Saved", "SaveGames")
	if err := addDirectoryToArchive(archive, saveRoot, "SaveGames"); err != nil {
		_ = archive.Close()
		return "", err
	}
	if err := addFileToArchive(archive, settingsFile, "PalWorldSettings.ini"); err != nil {
		_ = archive.Close()
		return "", err
	}
	if err := archive.Close(); err != nil {
		return "", err
	}
	if err := temp.Sync(); err != nil {
		return "", err
	}
	if err := temp.Close(); err != nil {
		return "", err
	}
	if err := replaceFile(tempPath, finalPath); err != nil {
		return "", err
	}
	ok = true
	return finalPath, nil
}

func pruneBackups(
	backupDir, keepPath string,
	maxAge time.Duration,
	maxTotalBytes int64,
	now time.Time,
) (backupRetentionResult, error) {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return backupRetentionResult{}, fmt.Errorf("read backup directory for retention: %w", err)
	}
	backups := make([]managedBackup, 0, len(entries))
	var problems []error
	var totalBytes int64
	for _, entry := range entries {
		createdAt, ok := managedBackupTime(entry.Name(), now.Location())
		if !ok || entry.IsDir() {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			problems = append(problems, fmt.Errorf("inspect backup %s: %w", entry.Name(), infoErr))
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		backup := managedBackup{
			path:      filepath.Join(backupDir, entry.Name()),
			createdAt: createdAt,
			size:      info.Size(),
		}
		backups = append(backups, backup)
		totalBytes += backup.size
	}
	sort.Slice(backups, func(left, right int) bool {
		return backups[left].createdAt.Before(backups[right].createdAt)
	})

	result := backupRetentionResult{}
	cleanKeepPath := filepath.Clean(keepPath)
	cutoff := now.Add(-maxAge)
	for _, backup := range backups {
		if filepath.Clean(backup.path) == cleanKeepPath {
			continue
		}
		expired := maxAge > 0 && backup.createdAt.Before(cutoff)
		overCapacity := maxTotalBytes > 0 && totalBytes > maxTotalBytes
		if !expired && !overCapacity {
			continue
		}
		if removeErr := os.Remove(backup.path); removeErr != nil {
			problems = append(problems, fmt.Errorf("remove retained backup %s: %w", filepath.Base(backup.path), removeErr))
			continue
		}
		result.Removed++
		result.FreedBytes += backup.size
		totalBytes -= backup.size
	}
	return result, errors.Join(problems...)
}

func managedBackupTime(name string, location *time.Location) (time.Time, bool) {
	if !strings.HasPrefix(name, "palworld-") || !strings.HasSuffix(name, ".zip") {
		return time.Time{}, false
	}
	timestamp := strings.TrimSuffix(strings.TrimPrefix(name, "palworld-"), ".zip")
	parsed, err := time.ParseInLocation(backupTimestampLayout, timestamp, location)
	return parsed, err == nil
}

func addDirectoryToArchive(archive *zip.Writer, root, archiveRoot string) error {
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("open Palworld save directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("Palworld save path is not a directory: %s", root)
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(filepath.Join(archiveRoot, relative))
		if strings.HasPrefix(name, "../") {
			return fmt.Errorf("backup path escaped save directory: %s", path)
		}
		return addFileToArchive(archive, path, name)
	})
}

func addFileToArchive(archive *zip.Writer, path, name string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name = filepath.ToSlash(name)
	header.Method = zip.Deflate
	writer, err := archive.CreateHeader(header)
	if err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(writer, file)
	return err
}
