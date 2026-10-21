package shared

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
)

// MaxBackupSnapshots limits history for each source file, independently in
// each module. First-original snapshots are never part of this history.
const MaxBackupSnapshots = 10

var snapshotSuffix = regexp.MustCompile(`^([0-9]{8}_[0-9]{6})-[0-9]+$`)

func isSnapshotForSource(name, source string) bool {
	prefix := filepath.Base(source) + ".bak."
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	parts := snapshotSuffix.FindStringSubmatch(strings.TrimPrefix(name, prefix))
	if len(parts) == 0 {
		return false
	}
	_, err := time.Parse("20060102_150405", parts[1])
	return err == nil
}

func (store BackupStore) pruneSnapshots(source, latest string) error {
	dir, err := store.Directory(source)
	if err != nil {
		return err
	}
	children, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var entries []BackupEntry
	foundLatest := false
	for _, child := range children {
		if !isSnapshotForSource(child.Name(), source) {
			continue
		}
		path := filepath.Join(dir, child.Name())
		info, exists, err := validateAtomicTarget(path)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("整理备份期间文件发生变化: %s", path)
		}
		entries = append(entries, BackupEntry{Path: path, info: info})
		foundLatest = foundLatest || path == latest
	}
	if !foundLatest {
		return fmt.Errorf("本次新备份已被其他操作移除: %s", latest)
	}
	if len(entries) <= MaxBackupSnapshots {
		return nil
	}
	sort.Slice(entries, func(i, j int) bool {
		// Always preserve the newly saved backup, even after clock rollback.
		if entries[i].Path == latest || entries[j].Path == latest {
			return entries[i].Path == latest
		}
		if entries[i].ModTime().Equal(entries[j].ModTime()) {
			return entries[i].Path > entries[j].Path
		}
		return entries[i].ModTime().After(entries[j].ModTime())
	})
	_, err = store.Delete(entries[MaxBackupSnapshots:])
	return err
}

// Serialize snapshot creation and pruning across processes. The lock lives in
// the private backup directory and is excluded from backup inventories.
func lockBackupDirectory(dir string) (func(), error) {
	path := filepath.Join(dir, ".snailtool-backup.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, fmt.Errorf("打开备份锁失败: %w", err)
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, fmt.Errorf("备份锁不是普通文件: %s", path)
	}
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("锁定备份目录失败: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}, nil
}
