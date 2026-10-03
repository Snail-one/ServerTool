package golang

import (
	"os"
	"path/filepath"
	"testing"
)

func writeManagedVersion(t *testing.T, path, binary string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(path, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "bin", "go"), []byte(binary), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, managedFile), []byte("managed"), 0644); err != nil {
		t.Fatal(err)
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
