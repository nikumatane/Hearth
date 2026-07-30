package palworld

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func createBackup(installDir, settingsFile, backupDir string) (string, error) {
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return "", fmt.Errorf("create backup directory: %w", err)
	}
	finalPath := filepath.Join(backupDir, "palworld-"+time.Now().Format("20060102-150405.000000000")+".zip")
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
