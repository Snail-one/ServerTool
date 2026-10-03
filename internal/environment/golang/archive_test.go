package golang

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
