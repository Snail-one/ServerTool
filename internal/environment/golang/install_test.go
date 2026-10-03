package golang

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceManagedVersion(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "go1.24.1")
	replacement := filepath.Join(root, ".repair")
	writeManagedVersion(t, destination, "old")
	writeManagedVersion(t, replacement, "new")

	if err := replaceManagedVersion(destination, replacement); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(destination, "bin", "go")); got != "new" {
		t.Fatalf("replacement binary = %q", got)
	}
	if _, err := os.Stat(replacement); !os.IsNotExist(err) {
		t.Fatalf("replacement path still exists: %v", err)
	}
}

func TestReplaceManagedVersionRefusesUnmanagedReplacement(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "go1.24.1")
	replacement := filepath.Join(root, ".repair")
	writeManagedVersion(t, destination, "old")
	if err := os.MkdirAll(filepath.Join(replacement, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(replacement, "bin", "go"), []byte("new"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := replaceManagedVersion(destination, replacement); err == nil {
		t.Fatal("expected unmanaged replacement to be rejected")
	}
	if got := readFile(t, filepath.Join(destination, "bin", "go")); got != "old" {
		t.Fatalf("original version changed: %q", got)
	}
}
