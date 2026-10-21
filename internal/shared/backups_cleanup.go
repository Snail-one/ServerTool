package shared

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// BackupEntry records the file presented for confirmation. Deletion checks
// that the same file is still present and leaves subsequently created files.
type BackupEntry struct {
	Path string
	info os.FileInfo
}

func (entry BackupEntry) Size() int64 { return entry.info.Size() }

func (store BackupStore) moduleDirectory() (string, error) {
	if store.Root == "" || !filepath.IsAbs(store.Root) || store.Module == "" || store.Module == "." || store.Module == ".." || filepath.Base(store.Module) != store.Module {
		return "", fmt.Errorf("备份目录或模块名称无效")
	}
	return filepath.Join(store.Root, "backups", store.Module), nil
}

func (store BackupStore) List() ([]BackupEntry, error) {
	dir, err := store.moduleDirectory()
	if err != nil {
		return nil, err
	}
	exists, err := validateRemovalTarget(dir)
	if err != nil || !exists {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("备份模块路径不是目录: %s", dir)
	}
	var entries []BackupEntry
	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("备份目录存在非普通文件，已停止清理: %s", path)
		}
		// Keep in-progress original snapshots and unrelated files untouched.
		if isBackupName(entry.Name()) {
			entries = append(entries, BackupEntry{Path: path, info: info})
		}
		return nil
	})
	return entries, err
}

func isBackupName(name string) bool {
	return !strings.HasPrefix(name, ".") && (strings.Contains(name, ".bak.") || strings.HasSuffix(name, ".original.json"))
}

func (store BackupStore) CheckEntries(entries []BackupEntry) error {
	dir, err := store.moduleDirectory()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		relative, err := filepath.Rel(dir, entry.Path)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || !isBackupName(filepath.Base(entry.Path)) || entry.info == nil {
			return fmt.Errorf("备份清理路径无效: %s", entry.Path)
		}
		info, exists, err := validateAtomicTarget(entry.Path)
		if err != nil {
			return err
		}
		if !exists || !os.SameFile(entry.info, info) || entry.info.Size() != info.Size() || !entry.info.ModTime().Equal(info.ModTime()) {
			return fmt.Errorf("备份在确认期间发生变化，未继续清理，请刷新后重试: %s", entry.Path)
		}
	}
	return nil
}

func (store BackupStore) Delete(entries []BackupEntry) (int, error) {
	if err := store.CheckEntries(entries); err != nil {
		return 0, err
	}
	for index, entry := range entries {
		if err := store.CheckEntries([]BackupEntry{entry}); err != nil {
			return index, err
		}
		if err := RemoveRegularFile(entry.Path); err != nil {
			return index, err
		}
	}
	return len(entries), nil
}
