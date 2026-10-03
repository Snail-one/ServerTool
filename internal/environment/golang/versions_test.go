package golang

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestVersionInstallPathUsesManagedRoot(t *testing.T) {
	if got := versionInstallPath("go1.24.1"); got != "/opt/go/go1.24.1" {
		t.Fatalf("versionInstallPath() = %q", got)
	}
}

func TestInstalledVersionsAndActivation(t *testing.T) {
	root := t.TempDir()
	for _, version := range []string{"go1.9.9", "go1.24.1", "invalid"} {
		path := filepath.Join(root, version, "bin")
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "go"), []byte("binary"), 0755); err != nil {
			t.Fatal(err)
		}
		if version != "invalid" {
			if err := os.WriteFile(filepath.Join(root, version, managedFile), []byte("managed"), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	versions, err := installedVersions(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"go1.24.1", "go1.9.9"}
	if !reflect.DeepEqual(versions, want) {
		t.Fatalf("installed versions = %#v, want %#v", versions, want)
	}

	link := filepath.Join(root, "current")
	if err := activateVersion(root, link, "go1.9.9"); err != nil {
		t.Fatal(err)
	}
	if got := activeVersion(link); got != "go1.9.9" {
		t.Fatalf("active version = %q", got)
	}
	if err := activateVersion(root, link, "go1.24.1"); err != nil {
		t.Fatal(err)
	}
	if got := activeVersion(link); got != "go1.24.1" {
		t.Fatalf("active version after switch = %q", got)
	}
}

func TestActivateVersionRefusesNonSymlink(t *testing.T) {
	root := t.TempDir()
	goBinary := filepath.Join(root, "go1.24.1", "bin", "go")
	if err := os.MkdirAll(filepath.Dir(goBinary), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goBinary, nil, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go1.24.1", managedFile), []byte("managed"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "current")
	if err := os.WriteFile(link, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := activateVersion(root, link, "go1.24.1"); err == nil {
		t.Fatal("expected activation to refuse replacing a regular file")
	}
	data, err := os.ReadFile(link)
	if err != nil || string(data) != "keep" {
		t.Fatalf("existing path was changed: %q, %v", data, err)
	}
}

func TestActivateVersionRefusesExternalSymlink(t *testing.T) {
	root := t.TempDir()
	goBinary := filepath.Join(root, "go1.24.1", "bin", "go")
	if err := os.MkdirAll(filepath.Dir(goBinary), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goBinary, nil, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go1.24.1", managedFile), []byte("managed"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "current")
	if err := os.Symlink(filepath.Join(t.TempDir(), "go1.23.1"), link); err != nil {
		t.Fatal(err)
	}
	if err := activateVersion(root, link, "go1.24.1"); err == nil {
		t.Fatal("expected activation to refuse replacing an external symlink")
	}
}

func TestInstallArtifactsOnlyReturnsKnownTemporaryEntries(t *testing.T) {
	root := t.TempDir()
	wanted := []string{
		".backup-go1.24.1-123",
		".current.tmp",
		".download-123.tar.gz",
		".install-123",
		".repair-123",
	}
	other := []string{"current", "go1.24.1", ".download-not-an-archive", "notes.txt"}
	for _, name := range append(append([]string{}, wanted...), other...) {
		if err := os.MkdirAll(filepath.Join(root, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	got, err := installArtifacts(root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, wanted) {
		t.Fatalf("install artifacts = %#v, want %#v", got, wanted)
	}
}
