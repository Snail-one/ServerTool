package shared

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupCleanupDeletesOnlyConfirmedSnapshots(t *testing.T) {
	store := BackupStore{Root: t.TempDir(), Module: "firewall"}
	source := filepath.Join(t.TempDir(), "nftables.conf")
	if err := os.WriteFile(source, []byte("live configuration"), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := store.Snapshot(source)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := store.List()
	if err != nil || len(entries) != 1 || entries[0].Size() != int64(len("live configuration")) {
		t.Fatalf("incorrect inventory: %v %v", entries, err)
	}
	second, err := store.Snapshot(source)
	if err != nil {
		t.Fatal(err)
	}
	if count, err := store.Delete(entries); err != nil || count != 1 {
		t.Fatalf("delete: %d %v", count, err)
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatalf("confirmed backup remains: %v", err)
	}
	if ReadFileString(second) != "live configuration" || ReadFileString(source) != "live configuration" {
		t.Fatal("new backup or live configuration was affected")
	}
}

func TestBackupCleanupRejectsChangedFilesBeforeAnyRemoval(t *testing.T) {
	store := BackupStore{Root: t.TempDir(), Module: "ssh"}
	source := filepath.Join(t.TempDir(), "sshd_config")
	if err := os.WriteFile(source, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := store.Snapshot(source); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := store.List()
	if err != nil || len(entries) != 2 {
		t.Fatalf("inventory: %v %v", entries, err)
	}
	changed := entries[1].Path
	if err := AtomicWriteFile(changed, []byte("replaced"), AtomicWriteOptions{Mode: 0600}); err != nil {
		t.Fatal(err)
	}
	if count, err := store.Delete(entries); err == nil || count != 0 {
		t.Fatalf("changed snapshot must stop all deletions: %d %v", count, err)
	}
	if ReadFileString(entries[0].Path) != "original" || ReadFileString(changed) != "replaced" {
		t.Fatal("snapshot was deleted despite concurrent modification")
	}
}

func TestBackupCleanupRejectsSymlinksAndKeepsOtherData(t *testing.T) {
	store := BackupStore{Root: t.TempDir(), Module: "ups"}
	source := filepath.Join(t.TempDir(), "ups.conf")
	if err := os.WriteFile(source, []byte("original"), 0640); err != nil {
		t.Fatal(err)
	}
	original, _, err := store.KeepOriginal(source)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(original)
	temporary := filepath.Join(dir, ".snailtool-original-in-progress")
	unrelated := filepath.Join(dir, "notes.txt")
	for _, path := range []string{temporary, unrelated} {
		if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(dir, "outside.bak.link")
	if err := os.Symlink(source, link); err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(); err == nil {
		t.Fatal("symbolic link in backup tree accepted")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	entries, err := store.List()
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary or unrelated data included: %v %v", entries, err)
	}
	wrongStore := BackupStore{Root: store.Root, Module: "ssh"}
	if _, err := wrongStore.Delete(entries); err == nil {
		t.Fatal("cross-module backup deletion accepted")
	}
	if _, err := store.Delete(entries); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{temporary, unrelated} {
		if ReadFileString(path) != "keep" {
			t.Fatal("unrelated data was removed")
		}
	}
	if ReadFileString(source) != "original" {
		t.Fatal("live UPS configuration was removed")
	}
}
