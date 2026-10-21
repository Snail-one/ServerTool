package shared

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func retentionSource(t *testing.T) (BackupStore, string) {
	t.Helper()
	store := BackupStore{Root: filepath.Join(t.TempDir(), "snailtool"), Module: "ssh"}
	source := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(source, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	return store, source
}

func TestSnapshotRetentionKeepsLatestTenAndOriginalPerSource(t *testing.T) {
	store, source := retentionSource(t)
	original, _, err := store.KeepOriginal(source)
	if err != nil {
		t.Fatal(err)
	}
	otherSource := filepath.Join(filepath.Dir(source), "config.other")
	if err := os.WriteFile(otherSource, []byte("other file"), 0600); err != nil {
		t.Fatal(err)
	}
	other, err := store.Snapshot(otherSource)
	if err != nil {
		t.Fatal(err)
	}
	var snapshots []string
	// Future dates also exercise preserving the new snapshot after a clock
	// rollback: the new file is older by mtime than every previous snapshot.
	start := time.Now().Add(time.Hour)
	for index := 0; index < MaxBackupSnapshots+5; index++ {
		if err := os.WriteFile(source, []byte(fmt.Sprintf("version %d", index)), 0600); err != nil {
			t.Fatal(err)
		}
		path, err := store.Snapshot(source)
		if err != nil {
			t.Fatal(err)
		}
		date := start.Add(time.Duration(index) * time.Second)
		if err := os.Chtimes(path, date, date); err != nil {
			t.Fatal(err)
		}
		snapshots = append(snapshots, path)
	}
	for index, path := range snapshots {
		data, err := os.ReadFile(path)
		if index < len(snapshots)-MaxBackupSnapshots {
			if !os.IsNotExist(err) {
				t.Fatalf("expired snapshot remains: %s %v", path, err)
			}
		} else if err != nil || string(data) != fmt.Sprintf("version %d", index) {
			t.Fatalf("recent snapshot lost: %d %s %v", index, data, err)
		}
	}
	if ReadFileString(other) != "other file" {
		t.Fatal("another source's backup was pruned")
	}
	if _, err := os.Stat(original); err != nil {
		t.Fatal("first-original backup was pruned")
	}
	backup, err := store.ReadOriginal(source)
	if err != nil || string(backup.Data) != "original" {
		t.Fatalf("first-original snapshot changed: %v %v", backup, err)
	}
}

func TestFailedNewSnapshotNeverPrunesExistingHistory(t *testing.T) {
	store, source := retentionSource(t)
	dir, err := store.ensureDirectory(source)
	if err != nil {
		t.Fatal(err)
	}
	var old []string
	for index := 0; index < MaxBackupSnapshots+2; index++ {
		path := filepath.Join(dir, fmt.Sprintf("config.bak.20200101_000000-%d", index))
		if err := os.WriteFile(path, []byte("old snapshot"), 0600); err != nil {
			t.Fatal(err)
		}
		old = append(old, path)
	}
	previousSync := syncAtomicDir
	syncAtomicDir = func(*os.File) error { return errors.New("injected directory sync failure") }
	t.Cleanup(func() { syncAtomicDir = previousSync })
	if _, err := store.Snapshot(source); err == nil {
		t.Fatal("expected snapshot failure")
	}
	for _, path := range old {
		if ReadFileString(path) != "old snapshot" {
			t.Fatalf("failed snapshot pruned prior history: %s", path)
		}
	}
	syncAtomicDir = previousSync
	if _, err := store.Snapshot(source); err != nil {
		t.Fatal(err)
	}
	entries, err := store.List()
	if err != nil || len(entries) != MaxBackupSnapshots {
		t.Fatalf("successful next snapshot did not apply limit: %d %v", len(entries), err)
	}
}

func TestConcurrentSnapshotsRespectRetentionAndRemainComplete(t *testing.T) {
	store, source := retentionSource(t)
	var group sync.WaitGroup
	results := make(chan error, MaxBackupSnapshots*3)
	for index := 0; index < cap(results); index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := store.Snapshot(source)
			results <- err
		}()
	}
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	entries, err := store.List()
	if err != nil || len(entries) != MaxBackupSnapshots {
		t.Fatalf("concurrent history count=%d err=%v", len(entries), err)
	}
	for _, entry := range entries {
		if ReadFileString(entry.Path) != "original" {
			t.Fatal("retained snapshot is incomplete")
		}
	}
}

func TestRetentionRejectsSymlinksAndPreservesUnrelatedFiles(t *testing.T) {
	store, source := retentionSource(t)
	first, err := store.Snapshot(source)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(first)
	manual := filepath.Join(dir, "config.bak.manual")
	if err := os.WriteFile(manual, []byte("manual backup"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "config.bak.20200101_000000-999")
	if err := os.Symlink(source, link); err != nil {
		t.Fatal(err)
	}
	latest, err := store.Snapshot(source)
	if err == nil || ReadFileString(latest) != "original" || ReadFileString(first) != "original" || ReadFileString(manual) != "manual backup" || ReadFileString(source) != "original" {
		t.Fatalf("unsafe retention or missing new backup: %s %v", latest, err)
	}
}
