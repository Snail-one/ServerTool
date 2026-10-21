package shared

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func TestBackupSnapshotsKeepSourcePathsAndPrivatePermissions(t *testing.T) {
	store := BackupStore{Root: filepath.Join(t.TempDir(), "snailtool"), Module: "ssh"}
	sourceRoot := t.TempDir()
	var backups []string
	for _, directory := range []string{"one", "two"} {
		source := filepath.Join(sourceRoot, directory, "config")
		if err := os.MkdirAll(filepath.Dir(source), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(source, []byte(directory), 0644); err != nil {
			t.Fatal(err)
		}
		first, err := store.Snapshot(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(source, []byte("updated"), 0644); err != nil {
			t.Fatal(err)
		}
		second, err := store.Snapshot(source)
		if err != nil {
			t.Fatal(err)
		}
		if first == second || ReadFileString(first) != directory || ReadFileString(second) != "updated" {
			t.Fatal("snapshot content was lost or overwritten")
		}
		backups = append(backups, first)
		for _, path := range []string{first, second} {
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatalf("backup should be private: %v %v", info, err)
			}
			for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
				info, err := os.Stat(dir)
				if err != nil || info.Mode().Perm() != 0700 {
					t.Fatalf("data directory should be private: %s %v %v", dir, info, err)
				}
				if dir == store.Root {
					break
				}
			}
		}
		if matches, _ := filepath.Glob(source + ".bak.*"); len(matches) != 0 {
			t.Fatalf("backup was written beside the source: %v", matches)
		}
	}
	if filepath.Dir(backups[0]) == filepath.Dir(backups[1]) {
		t.Fatal("same-named sources must retain distinct source directories")
	}
}

func TestKeepOriginalPreservesMetadataAndPublishesOnlyOnce(t *testing.T) {
	store := BackupStore{Root: filepath.Join(t.TempDir(), "snailtool"), Module: "ups"}
	source := filepath.Join(t.TempDir(), "ups.conf")
	if err := os.WriteFile(source, []byte("first\n"), 0640); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(source)
	wantOwner, err := fileOwner(info)
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		path    string
		created bool
		err     error
	}
	results := make(chan result, 8)
	var group sync.WaitGroup
	for i := 0; i < cap(results); i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			path, created, err := store.KeepOriginal(source)
			results <- result{path, created, err}
		}()
	}
	group.Wait()
	close(results)
	createdCount := 0
	wantPath, _ := store.OriginalPath(source)
	for got := range results {
		if got.err != nil || got.path != wantPath {
			t.Fatalf("concurrent original backup failed: %+v", got)
		}
		if got.created {
			createdCount++
		}
	}
	if createdCount != 1 {
		t.Fatalf("created %d original backups, want exactly one", createdCount)
	}
	if err := os.WriteFile(source, []byte("second\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if _, created, err := store.KeepOriginal(source); err != nil || created {
		t.Fatalf("original was replaced: %v %v", created, err)
	}
	backup, err := store.ReadOriginal(source)
	if err != nil || string(backup.Data) != "first\n" || backup.Mode != 0640 || !reflect.DeepEqual(backup.Owner, wantOwner) {
		t.Fatalf("original content or metadata lost: %+v %v", backup, err)
	}
	if matches, _ := filepath.Glob(filepath.Join(filepath.Dir(wantPath), ".snailtool-original-*")); len(matches) != 0 {
		t.Fatalf("temporary backup files leaked: %v", matches)
	}
}

func TestOriginalBackupRejectsCorruptionWithoutReplacingIt(t *testing.T) {
	for _, contents := range []string{"not json", `{"source":"/wrong/file","mode":416,"data":"Zmlyc3Q="}`} {
		t.Run(contents, func(t *testing.T) {
			store := BackupStore{Root: filepath.Join(t.TempDir(), "snailtool"), Module: "ups"}
			source := filepath.Join(t.TempDir(), "ups.conf")
			if err := os.WriteFile(source, []byte("current"), 0640); err != nil {
				t.Fatal(err)
			}
			path, _, err := store.KeepOriginal(source)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := store.ReadOriginal(source); err == nil {
				t.Fatal("corrupt original was accepted")
			}
			if _, _, err := store.KeepOriginal(source); err == nil || ReadFileString(path) != contents {
				t.Fatal("corrupt original must be reported and left untouched")
			}
		})
	}
}

func TestBackupStoreRejectsSymlinksAndInvalidPaths(t *testing.T) {
	store := BackupStore{Root: filepath.Join(t.TempDir(), "snailtool"), Module: "ups"}
	source := filepath.Join(t.TempDir(), "ups.conf")
	if err := os.WriteFile(source, []byte("config"), 0640); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := os.Symlink(target, store.Root); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Snapshot(source); err == nil {
		t.Fatal("symlink data directory accepted")
	}
	if _, _, err := store.KeepOriginal(source); err == nil {
		t.Fatal("original backup followed a symlink")
	}
	entries, err := os.ReadDir(target)
	if err != nil || len(entries) != 0 {
		t.Fatalf("symlink target was modified: %v %v", entries, err)
	}
	for _, module := range []string{"", "..", "../outside", "/absolute"} {
		invalid := BackupStore{Root: t.TempDir(), Module: module}
		if _, err := invalid.Snapshot(source); err == nil {
			t.Fatalf("invalid module %q accepted", module)
		}
	}
	if _, err := store.Directory("relative.conf"); err == nil {
		t.Fatal("relative source accepted")
	}
	// A snapshot for a different source must never be restored.
	valid := BackupStore{Root: t.TempDir(), Module: "ups"}
	path, _, err := valid.KeepOriginal(source)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(OriginalBackup{Source: source, Mode: 0640, Data: []byte("data")})
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "other.json")
	if err := os.WriteFile(other, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, path); err != nil {
		t.Fatal(err)
	}
	if _, err := valid.ReadOriginal(source); err == nil {
		t.Fatal("symlink backup accepted")
	}
	if err := valid.RemoveOriginal(source); err == nil || ReadFileString(other) != string(data) {
		t.Fatal("symlink backup must not be removed or followed")
	}
}
