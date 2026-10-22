package golang

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGoDeletionRefusesParentSymlinkAndReplacement(t *testing.T) {
	root := t.TempDir()
	version := "go1.24.1"
	path := filepath.Join(root, version)
	writeManagedVersion(t, path, "original")
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "redirected")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if err := removeManagedVersion(link, version, info); err == nil {
		t.Fatal("redirected managed root accepted")
	}
	if err := removeOfficialInstall(filepath.Join(link, version)); err == nil {
		t.Fatal("redirected official path accepted")
	}
	if err := os.Rename(path, path+"-saved"); err != nil {
		t.Fatal(err)
	}
	writeManagedVersion(t, path, "replacement")
	if err := removeManagedVersion(root, version, info); err == nil {
		t.Fatal("replacement version deleted")
	}
	if readFile(t, filepath.Join(path, "bin/go")) != "replacement" {
		t.Fatal("replacement changed")
	}
}

func TestGoArtifactDeletionRejectsWrongTypesAndReplacement(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{".install-notes", ".backup-go1.24.1-123", ".current.tmp", ".download-123.tar.gz"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := installArtifacts(root); err != nil || len(got) != 0 {
		t.Fatalf("unrelated entries offered for deletion: %v %v", got, err)
	}
	path := filepath.Join(root, ".install-123")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	info, err := installArtifactInfo(root, filepath.Base(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+"-saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := removeInstallArtifact(root, filepath.Base(path), info); err == nil {
		t.Fatal("replacement artifact deleted")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("replacement artifact removed")
	}
}

func TestGoActivationDoesNotDeleteFixedTemporaryUserFile(t *testing.T) {
	root := t.TempDir()
	writeManagedVersion(t, filepath.Join(root, "go1.24.1"), "binary")
	path := filepath.Join(root, ".current.tmp")
	if err := os.WriteFile(path, []byte("manual data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := activateVersion(root, filepath.Join(root, "current"), "go1.24.1"); err != nil {
		t.Fatal(err)
	}
	if readFile(t, path) != "manual data" {
		t.Fatal("fixed temporary user file deleted")
	}
}
