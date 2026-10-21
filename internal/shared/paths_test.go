package shared

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsurePrivateDirCreatesAndRestrictsDataDirectory(t *testing.T) {
	parent := t.TempDir()
	before, err := os.Stat(parent)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "snailtool", "backups", "firewall")
	if err := EnsurePrivateDir(path); err != nil {
		t.Fatal(err)
	}
	for dir := path; dir != parent; dir = filepath.Dir(dir) {
		info, err := os.Stat(dir)
		if err != nil || info.Mode().Perm() != 0700 {
			t.Fatalf("directory %s should be private: %v %v", dir, info, err)
		}
	}
	if err := os.Chmod(path, 0755); err != nil {
		t.Fatal(err)
	}
	if err := EnsurePrivateDir(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("existing data directory should be restricted: %v %v", info, err)
	}
	after, err := os.Stat(parent)
	if err != nil || after.Mode() != before.Mode() {
		t.Fatalf("ancestor permissions changed: %v %v", after, err)
	}
}

func TestEnsurePrivateDirRejectsSymlinksAndFiles(t *testing.T) {
	for _, kind := range []string{"symlink", "file"} {
		t.Run(kind, func(t *testing.T) {
			parent := t.TempDir()
			target := t.TempDir()
			path := filepath.Join(parent, "snailtool")
			if kind == "symlink" {
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := EnsurePrivateDir(filepath.Join(path, "backups")); err == nil {
				t.Fatal("invalid data directory accepted")
			}
			entries, err := os.ReadDir(target)
			if err != nil || len(entries) != 0 {
				t.Fatalf("symlink target must not be modified: %v %v", entries, err)
			}
		})
	}
}
