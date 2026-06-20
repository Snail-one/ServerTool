package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFindComposeDirsMatchesScriptDepthAndDedupes(t *testing.T) {
	root := t.TempDir()
	appOne := filepath.Join(root, "app-one")
	appTwo := filepath.Join(root, "app-two")

	writeTestFile(t, filepath.Join(appOne, "docker-compose.yml"), "services: {}\n")
	writeTestFile(t, filepath.Join(appOne, "compose.yaml"), "services: {}\n")
	writeTestFile(t, filepath.Join(appTwo, "compose.yml"), "services: {}\n")
	writeTestFile(t, filepath.Join(root, "docker-compose.yml"), "services: {}\n")
	writeTestFile(t, filepath.Join(root, "group", "app-three", "docker-compose.yml"), "services: {}\n")
	writeTestFile(t, filepath.Join(root, "app-four", "Dockerfile"), "FROM scratch\n")

	got, err := findComposeDirs(root)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{appOne, appTwo}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("compose dirs mismatch\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestFindComposeDirsReturnsEmptyWhenNoneFound(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "app", "Dockerfile"), "FROM scratch\n")

	got, err := findComposeDirs(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no compose dirs, got %#v", got)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
