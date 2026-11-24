package golang

import (
	"os"
	"path/filepath"
	"testing"
)

func commandLinkTestInstall(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	version := filepath.Join(root, "go1.24.1")
	writeManagedVersion(t, version, "first version")
	if err := os.WriteFile(filepath.Join(version, "bin", "gofmt"), []byte("gofmt"), 0755); err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(root, "current")
	if err := activateVersion(root, current, "go1.24.1"); err != nil {
		t.Fatal(err)
	}
	return root, current, filepath.Join(t.TempDir(), "bin")
}

func TestGoCommandLinksFollowVersionSwitchAndAreIdempotent(t *testing.T) {
	root, current, binDir := commandLinkTestInstall(t)
	for i := 0; i < 2; i++ {
		if err := ensureGoCommandLinks(binDir, current); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"go", "gofmt"} {
		got, err := os.Readlink(filepath.Join(binDir, name))
		if want := filepath.Join(current, "bin", name); err != nil || got != want {
			t.Fatalf("%s target = %q, %v; want %q", name, got, err, want)
		}
	}
	writeManagedVersion(t, filepath.Join(root, "go1.25.1"), "second version")
	if err := activateVersion(root, current, "go1.25.1"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(binDir, "go")); got != "second version" {
		t.Fatalf("command link did not follow version switch: %q", got)
	}
}

func TestGoCommandLinksRefuseConflictsBeforeCreatingAnyLinks(t *testing.T) {
	for _, kind := range []string{"file", "directory", "foreign symlink"} {
		t.Run(kind, func(t *testing.T) {
			_, current, binDir := commandLinkTestInstall(t)
			if err := os.MkdirAll(binDir, 0755); err != nil {
				t.Fatal(err)
			}
			conflict := filepath.Join(binDir, "gofmt")
			switch kind {
			case "file":
				if err := os.WriteFile(conflict, []byte("keep"), 0755); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(conflict, 0755); err != nil {
					t.Fatal(err)
				}
			case "foreign symlink":
				if err := os.Symlink("/another/install/gofmt", conflict); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.Lstat(conflict)
			if err != nil {
				t.Fatal(err)
			}
			if err := ensureGoCommandLinks(binDir, current); err == nil {
				t.Fatal("expected existing command conflict to be rejected")
			}
			after, err := os.Lstat(conflict)
			if err != nil || !os.SameFile(before, after) {
				t.Fatalf("conflicting path was modified: %v", err)
			}
			if _, err := os.Lstat(filepath.Join(binDir, "go")); !os.IsNotExist(err) {
				t.Fatalf("go link must not be created when gofmt conflicts: %v", err)
			}
		})
	}
}

func TestGoCommandLinksRequireUsableCurrentCommands(t *testing.T) {
	for _, state := range []string{"missing current", "missing gofmt", "non-executable gofmt"} {
		t.Run(state, func(t *testing.T) {
			_, current, binDir := commandLinkTestInstall(t)
			switch state {
			case "missing current":
				if err := os.Remove(current); err != nil {
					t.Fatal(err)
				}
			case "missing gofmt":
				if err := os.Remove(filepath.Join(current, "bin", "gofmt")); err != nil {
					t.Fatal(err)
				}
			case "non-executable gofmt":
				if err := os.Chmod(filepath.Join(current, "bin", "gofmt"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if err := ensureGoCommandLinks(binDir, current); err == nil {
				t.Fatal("expected unusable current commands to be rejected")
			}
			if _, err := os.Lstat(binDir); !os.IsNotExist(err) {
				t.Fatalf("command directory was created despite invalid installation: %v", err)
			}
		})
	}
}

func TestCleanupGoCommandLinksRemovesDanglingOwnedLinks(t *testing.T) {
	_, current, binDir := commandLinkTestInstall(t)
	if err := ensureGoCommandLinks(binDir, current); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(current); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := cleanupGoCommandLinks(binDir, current); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"go", "gofmt"} {
		if _, err := os.Lstat(filepath.Join(binDir, name)); !os.IsNotExist(err) {
			t.Fatalf("owned %s link remained after cleanup: %v", name, err)
		}
	}
}

func TestCleanupGoCommandLinksPreservesOtherCommands(t *testing.T) {
	for _, kind := range []string{"file", "foreign symlink"} {
		t.Run(kind, func(t *testing.T) {
			_, current, binDir := commandLinkTestInstall(t)
			if err := ensureGoCommandLinks(binDir, current); err != nil {
				t.Fatal(err)
			}
			other := filepath.Join(binDir, "gofmt")
			if err := os.Remove(other); err != nil {
				t.Fatal(err)
			}
			if kind == "file" {
				if err := os.WriteFile(other, []byte("keep"), 0755); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Symlink("/another/install/gofmt", other); err != nil {
				t.Fatal(err)
			}
			before, err := os.Lstat(other)
			if err != nil {
				t.Fatal(err)
			}
			if err := cleanupGoCommandLinks(binDir, current); err != nil {
				t.Fatal(err)
			}
			after, err := os.Lstat(other)
			if err != nil || !os.SameFile(before, after) {
				t.Fatalf("unrelated command was modified during cleanup: %v", err)
			}
			if _, err := os.Lstat(filepath.Join(binDir, "go")); !os.IsNotExist(err) {
				t.Fatalf("owned go link remained after cleanup: %v", err)
			}
		})
	}
}
