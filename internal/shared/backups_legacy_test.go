package shared

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func legacyFile(t *testing.T, root, relative, content string) string {
	t.Helper()
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLegacyBackupCleanupFindsKnownLocationsAndPreservesOtherFiles(t *testing.T) {
	store := LegacyBackupStore{Root: t.TempDir()}
	var wanted []string
	for _, relative := range []string{
		"etc/nftables.conf.bak.20261021_051605",
		"etc/nftables.conf.bak.20261021_051606-123456",
		"etc/ssh/sshd_config.bak.20261021_051605",
		"etc/ssh/sshd_config.d/99-custom.conf.bak.20261021_051605",
		"etc/docker/daemon.json.bak.20261021_051605",
		"etc/nut/nut.conf.bak", "etc/nut/ups.conf.bak", "etc/nut/upsd.conf.bak",
		"etc/nut/upsd.users.bak", "etc/nut/upsmon.conf.bak", "etc/nut/upssched.conf.bak",
		"usr/local/sbin/ups-onbatt-actions.sh.bak",
	} {
		wanted = append(wanted, legacyFile(t, store.Root, relative, "old backup"))
	}
	var preserved []string
	for _, relative := range []string{
		"etc/nftables.conf", "etc/ssh/sshd_config", "etc/ssh/sshd_config.d/99-custom.conf",
		"etc/docker/daemon.json", "etc/nut/ups.conf", "usr/local/sbin/ups-onbatt-actions.sh",
		"etc/nftables.conf.bak.manual", "etc/nftables.conf.bak.20261321_999999",
		"etc/nftables.conf.bak.manual.bak.20261021_051605",
		"etc/nut/unrelated.conf.bak", "etc/unrelated.bak.20261021_051605",
		"etc/ssh/sshd_config.d/unrelated.txt.bak.20261021_051605",
		"var/lib/snailtool/backups/firewall/nftables.conf.bak.20261021_051605-789",
	} {
		preserved = append(preserved, legacyFile(t, store.Root, relative, "keep"))
	}
	entries, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, entry := range entries {
		got = append(got, entry.Path)
	}
	sort.Strings(wanted)
	if !reflect.DeepEqual(got, wanted) {
		t.Fatalf("legacy inventory=%v, want %v", got, wanted)
	}
	newBackup := legacyFile(t, store.Root, "etc/nftables.conf.bak.20261021_051607-456", "keep")
	preserved = append(preserved, newBackup)
	if count, err := store.Delete(entries); err != nil || count != len(wanted) {
		t.Fatalf("cleanup count=%d err=%v", count, err)
	}
	for _, path := range wanted {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("legacy backup not removed: %s %v", path, err)
		}
	}
	for _, path := range preserved {
		if ReadFileString(path) != "keep" {
			t.Fatalf("unlisted file affected: %s", path)
		}
	}
}

func TestLegacyBackupCleanupStopsIfConfirmedFileChanges(t *testing.T) {
	store := LegacyBackupStore{Root: t.TempDir()}
	first := legacyFile(t, store.Root, "etc/nftables.conf.bak.20261021_051605", "original")
	second := legacyFile(t, store.Root, "etc/ssh/sshd_config.bak.20261021_051605", "original")
	entries, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteFile(second, []byte("modified"), AtomicWriteOptions{Mode: 0600}); err != nil {
		t.Fatal(err)
	}
	if count, err := store.Delete(entries); err == nil || count != 0 {
		t.Fatalf("changed backup must prevent deletion: count=%d err=%v", count, err)
	}
	if ReadFileString(first) != "original" || ReadFileString(second) != "modified" {
		t.Fatal("a file was deleted before all snapshots were checked")
	}
	live := legacyFile(t, store.Root, "etc/nftables.conf", "live config")
	entries[0].Path = live
	if count, err := store.Delete(entries[:1]); err == nil || count != 0 || ReadFileString(live) != "live config" {
		t.Fatal("live configuration path must never be accepted for legacy cleanup")
	}
}

func TestLegacyBackupCleanupRejectsSymlinkFilesAndParents(t *testing.T) {
	for _, parent := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "directory"}[parent], func(t *testing.T) {
			store := LegacyBackupStore{Root: t.TempDir()}
			outside := t.TempDir()
			if parent {
				legacyFile(t, outside, "nftables.conf.bak.20261021_051605", "outside")
				if err := os.Symlink(outside, filepath.Join(store.Root, "etc")); err != nil {
					t.Fatal(err)
				}
			} else {
				target := legacyFile(t, outside, "file", "outside")
				path := legacyFile(t, store.Root, "etc/nftables.conf.bak.20261021_051605", "temporary")
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := store.List(); err == nil {
				t.Fatal("legacy scanning must reject symbolic links")
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 1 {
				t.Fatalf("outside files were changed: %v %v", entries, err)
			}
		})
	}
}
