package shared

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BackupStore keeps a module's backups below the shared persistent data root.
// Source directory names are retained to distinguish files with the same name.
type BackupStore struct {
	Root   string
	Module string
}

type OriginalBackup struct {
	Source string      `json:"source"`
	Mode   os.FileMode `json:"mode"`
	Owner  *FileOwner  `json:"owner,omitempty"`
	Data   []byte      `json:"data"`
}

func (store BackupStore) sourcePath(source string) (string, error) {
	dir, err := store.moduleDirectory()
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(source) || filepath.Clean(source) == string(os.PathSeparator) {
		return "", fmt.Errorf("备份来源必须为文件的绝对路径: %s", source)
	}
	return filepath.Join(dir, strings.TrimPrefix(filepath.Clean(source), string(os.PathSeparator))), nil
}

func (store BackupStore) Directory(source string) (string, error) {
	path, err := store.sourcePath(source)
	if err != nil {
		return "", err
	}
	return filepath.Dir(path), nil
}

func (store BackupStore) ensureDirectory(source string) (string, error) {
	dir, err := store.Directory(source)
	if err != nil {
		return "", err
	}
	// Restrict the root and every managed subdirectory, including existing ones.
	root := filepath.Clean(store.Root)
	current := root
	if err := EnsurePrivateDir(current); err != nil {
		return "", err
	}
	relative, _ := filepath.Rel(root, dir)
	for _, part := range strings.Split(relative, string(os.PathSeparator)) {
		current = filepath.Join(current, part)
		if err := EnsurePrivateDir(current); err != nil {
			return "", err
		}
	}
	return dir, nil
}

func readBackupSource(source string) (OriginalBackup, error) {
	info, exists, err := validateAtomicTarget(source)
	if err != nil {
		return OriginalBackup{}, err
	}
	if !exists {
		return OriginalBackup{}, fmt.Errorf("备份来源不存在: %s", source)
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return OriginalBackup{}, err
	}
	owner, err := fileOwner(info)
	if err != nil {
		return OriginalBackup{}, err
	}
	return OriginalBackup{Source: filepath.Clean(source), Mode: info.Mode().Perm(), Owner: owner, Data: data}, nil
}

// Snapshot writes a new private file for every operation, without overwriting
// earlier snapshots even when several are created in the same second.
func (store BackupStore) Snapshot(source string) (string, error) {
	backup, err := readBackupSource(source)
	if err != nil {
		return "", err
	}
	dir, err := store.ensureDirectory(source)
	if err != nil {
		return "", err
	}
	pattern := filepath.Base(source) + ".bak." + time.Now().Format("20060102_150405") + "-*"
	unlock, err := lockBackupDirectory(dir)
	if err != nil {
		return "", err
	}
	defer unlock()
	path, err := writeNewBackup(dir, pattern, backup.Data)
	if err != nil {
		return path, err
	}
	if err := store.pruneSnapshots(source, path); err != nil {
		return path, fmt.Errorf("新备份已保存，但清理超额备份失败: %w", err)
	}
	return path, nil
}

func writeNewBackup(dir, pattern string, data []byte) (path string, resultErr error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	path = file.Name()
	defer func() {
		_ = file.Close()
		if resultErr != nil {
			_ = os.Remove(path)
		}
	}()
	if err := file.Chmod(0600); err != nil {
		return path, err
	}
	if _, err := file.Write(data); err != nil {
		return path, err
	}
	if err := file.Sync(); err != nil {
		return path, err
	}
	if err := file.Close(); err != nil {
		return path, err
	}
	return path, syncBackupDirectory(dir)
}

func syncBackupDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return syncAtomicDir(dir)
}

func (store BackupStore) OriginalPath(source string) (string, error) {
	path, err := store.sourcePath(source)
	if err != nil {
		return "", err
	}
	return path + ".original.json", nil
}

// KeepOriginal preserves the first complete snapshot, including the original
// permissions and owner. Publishing via a hard link never replaces an existing
// snapshot, including when two processes try to create it concurrently.
func (store BackupStore) KeepOriginal(source string) (string, bool, error) {
	path, err := store.OriginalPath(source)
	if err != nil {
		return "", false, err
	}
	if _, err := store.ReadOriginal(source); err == nil {
		return path, false, nil
	} else if !os.IsNotExist(err) {
		return "", false, err
	}
	backup, err := readBackupSource(source)
	if err != nil {
		return "", false, err
	}
	dir, err := store.ensureDirectory(source)
	if err != nil {
		return "", false, err
	}
	data, err := json.Marshal(backup)
	if err != nil {
		return "", false, err
	}
	temporary, err := writeNewBackup(dir, ".snailtool-original-*", data)
	if err != nil {
		return "", false, err
	}
	defer os.Remove(temporary)
	if err := os.Link(temporary, path); err != nil {
		if os.IsExist(err) {
			_, readErr := store.ReadOriginal(source)
			return path, false, readErr
		}
		return "", false, err
	}
	return path, true, syncBackupDirectory(dir)
}

func (store BackupStore) ReadOriginal(source string) (OriginalBackup, error) {
	path, err := store.OriginalPath(source)
	if err != nil {
		return OriginalBackup{}, err
	}
	// Lstat lets a missing backup be detected without creating directories.
	info, err := os.Lstat(path)
	if err != nil {
		return OriginalBackup{}, err
	}
	if !info.Mode().IsRegular() {
		return OriginalBackup{}, fmt.Errorf("原始备份不是普通文件: %s", path)
	}
	if _, _, err := validateAtomicTarget(path); err != nil {
		return OriginalBackup{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return OriginalBackup{}, err
	}
	var backup OriginalBackup
	if err := json.Unmarshal(data, &backup); err != nil {
		return OriginalBackup{}, fmt.Errorf("原始备份损坏 %s: %w", path, err)
	}
	if backup.Source != filepath.Clean(source) || backup.Mode == 0 || backup.Mode != backup.Mode.Perm() || backup.Data == nil {
		return OriginalBackup{}, fmt.Errorf("原始备份元数据无效: %s", path)
	}
	return backup, nil
}

func (store BackupStore) RemoveOriginal(source string) error {
	path, err := store.OriginalPath(source)
	if err != nil {
		return err
	}
	return RemoveRegularFile(path)
}
