package shared

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// LegacyBackupStore is used only by explicit, confirmed legacy cleanup.
// Backup creation, restoration and automatic uninstall cleanup use BackupStore.
type LegacyBackupStore struct {
	Root string
}

var legacyBackupPatterns = []string{
	"/etc/nftables.conf.bak.*",
	"/etc/ssh/sshd_config.bak.*",
	"/etc/ssh/sshd_config.d/*.conf.bak.*",
	"/etc/docker/daemon.json.bak.*",
	"/etc/nut/nut.conf.bak",
	"/etc/nut/ups.conf.bak",
	"/etc/nut/upsd.conf.bak",
	"/etc/nut/upsd.users.bak",
	"/etc/nut/upsmon.conf.bak",
	"/etc/nut/upssched.conf.bak",
	"/usr/local/sbin/ups-onbatt-actions.sh.bak",
}

var legacyTimestamp = regexp.MustCompile(`\.bak\.([0-9]{8}_[0-9]{6})(-[0-9]+)?$`)

func (store LegacyBackupStore) patterns() ([]string, error) {
	if !filepath.IsAbs(store.Root) {
		return nil, fmt.Errorf("旧备份扫描根目录必须为绝对路径")
	}
	patterns := make([]string, 0, len(legacyBackupPatterns))
	for _, pattern := range legacyBackupPatterns {
		patterns = append(patterns, filepath.Join(store.Root, strings.TrimPrefix(pattern, "/")))
	}
	return patterns, nil
}

func matchesLegacyBackup(pattern, path string) bool {
	matched, err := filepath.Match(pattern, path)
	if err != nil || !matched {
		return false
	}
	if strings.HasSuffix(pattern, ".bak") {
		return true
	}
	parts := legacyTimestamp.FindStringSubmatch(filepath.Base(path))
	if len(parts) == 0 {
		return false
	}
	matched, err = filepath.Match(strings.TrimSuffix(pattern, ".bak.*"), strings.TrimSuffix(path, parts[0]))
	if err != nil || !matched {
		return false
	}
	_, err = time.Parse("20060102_150405", parts[1])
	return err == nil
}

func (store LegacyBackupStore) List() ([]BackupEntry, error) {
	patterns, err := store.patterns()
	if err != nil {
		return nil, err
	}
	var entries []BackupEntry
	for _, pattern := range patterns {
		dir := filepath.Dir(pattern)
		exists, err := validateRemovalTarget(dir)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		// ReadDir reports permission and I/O errors rather than silently
		// treating an unreadable directory as containing no matches.
		children, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, child := range children {
			path := filepath.Join(dir, child.Name())
			if !matchesLegacyBackup(pattern, path) {
				continue
			}
			info, exists, err := validateAtomicTarget(path)
			if err != nil {
				return nil, err
			}
			if exists {
				entries = append(entries, BackupEntry{Path: path, info: info})
			}
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

func (store LegacyBackupStore) CheckEntries(entries []BackupEntry) error {
	patterns, err := store.patterns()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		allowed := false
		for _, pattern := range patterns {
			if matchesLegacyBackup(pattern, entry.Path) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("文件不属于旧备份清理范围: %s", entry.Path)
		}
		if err := checkBackupEntry(entry); err != nil {
			return err
		}
	}
	return nil
}

func (store LegacyBackupStore) Delete(entries []BackupEntry) (int, error) {
	return deleteBackupEntries(entries, store.CheckEntries)
}
