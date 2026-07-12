package golang

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAvailableReleasesFiltersAndSorts(t *testing.T) {
	input := []release{
		{Version: "go1.9.9", Stable: true, Files: []releaseFile{archive("go1.9.9", "amd64")}},
		{Version: "go1.24.1", Stable: true, Files: []releaseFile{archive("go1.24.1", "amd64")}},
		{Version: "go1.24rc1", Stable: false, Files: []releaseFile{archive("go1.24rc1", "amd64")}},
		{Version: "go1.23.8", Stable: true, Files: []releaseFile{archive("go1.23.8", "arm64")}},
		{Version: "go1.24.1", Stable: true, Files: []releaseFile{archive("go1.24.1", "amd64")}},
	}

	got := availableReleases(input, "amd64")
	versions := make([]string, len(got))
	for i := range got {
		versions[i] = got[i].Version
	}
	want := []string{"go1.24.1", "go1.9.9"}
	if !reflect.DeepEqual(versions, want) {
		t.Fatalf("available versions = %#v, want %#v", versions, want)
	}
}

func TestVersionValidationAndComparison(t *testing.T) {
	valid := []string{"go1.22", "go1.22.0", "go1.100.12"}
	for _, version := range valid {
		if !validVersion(version) {
			t.Fatalf("expected %q to be valid", version)
		}
	}
	invalid := []string{"1.22.1", "go1", "go1.22rc1", "go1.02.1", "go1.2.3.4", "go1.x.1"}
	for _, version := range invalid {
		if validVersion(version) {
			t.Fatalf("expected %q to be invalid", version)
		}
	}
	if compareVersions("go1.24.1", "go1.9.10") <= 0 {
		t.Fatal("semantic comparison did not order go1.24.1 after go1.9.10")
	}
	if compareVersions("go1.22", "go1.22.0") != 0 {
		t.Fatal("minor version should compare equal to zero patch version")
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

func TestManagedPathIsIdempotentAndCleanable(t *testing.T) {
	bashrc := filepath.Join(t.TempDir(), ".bashrc")
	if err := os.WriteFile(bashrc, []byte("export EDITOR=vim\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeManagedPath(bashrc); err != nil {
		t.Fatal(err)
	}
	if err := writeManagedPath(bashrc); err != nil {
		t.Fatal(err)
	}
	content := readFile(t, bashrc)
	if strings.Count(content, pathBegin) != 1 || strings.Count(content, pathBody) != 1 {
		t.Fatalf("managed PATH block is not idempotent:\n%s", content)
	}
	if !strings.Contains(content, "export EDITOR=vim") {
		t.Fatalf("unrelated bashrc content was removed:\n%s", content)
	}
	changed, err := cleanupManagedPath(bashrc)
	if err != nil || !changed {
		t.Fatalf("cleanup = %v, %v", changed, err)
	}
	content = readFile(t, bashrc)
	if strings.Contains(content, pathBegin) || strings.Contains(content, pathBody) {
		t.Fatalf("managed PATH block remained:\n%s", content)
	}
	if !strings.Contains(content, "export EDITOR=vim") {
		t.Fatalf("unrelated bashrc content was removed:\n%s", content)
	}
}

func TestExtractGoArchiveRejectsTraversal(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "bad.tar.gz")
	writeTarGz(t, archivePath, map[string]string{"../escape": "bad"})
	err := extractGoArchive(archivePath, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "不安全路径") {
		t.Fatalf("expected unsafe path error, got %v", err)
	}
}

func TestExtractGoArchiveWritesGoBinary(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "go.tar.gz")
	writeTarGz(t, archivePath, map[string]string{"go/bin/go": "binary"})
	destination := t.TempDir()
	if err := extractGoArchive(archivePath, destination); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(destination, "go", "bin", "go")); got != "binary" {
		t.Fatalf("extracted content = %q", got)
	}
}

func archive(version, arch string) releaseFile {
	return releaseFile{
		Filename: version + ".linux-" + arch + ".tar.gz",
		OS:       "linux",
		Arch:     arch,
		Version:  version,
		SHA256:   "abc",
		Kind:     "archive",
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeTarGz(t *testing.T, path string, files map[string]string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gz)
	for name, content := range files {
		header := &tar.Header{Name: name, Mode: 0755, Size: int64(len(content)), Typeflag: tar.TypeReg}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
