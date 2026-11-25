package daemonlog

import (
	"errors"
	"os"
	"testing"

	"snail_tool/internal/storage"
	"snail_tool/internal/ui"
)

func TestCustomRotationBackReturnsToSizeAndPreset(t *testing.T) {
	view := navigationUI(t, "4\n100m\nq\nq\nq\nparent-next\n")
	_, skip, err := promptLogRotationConfig(view)
	if err != nil || !skip {
		t.Fatalf("skip=%v err=%v", skip, err)
	}
	if next, err := view.Ask("parent"); err != nil || next != "parent-next" {
		t.Fatalf("custom back skipped a page: next=%q err=%v", next, err)
	}
}
func TestOverwriteBackReturnsToLastRotationInputWithoutWriting(t *testing.T) {
	path := writeDaemonJSONFile(t, `{"log-driver":"json-file","log-opts":{"max-size":"10m"}}`)
	original := storage.ReadFileString(path)
	view := navigationUI(t, "4\n100m\n3\nq\nq\nq\nq\nparent-next\n")
	err := configureDockerLogRotation(view, path, "", func() bool { return true }, func(string) bool { return true },
		func(string, ...string) error { t.Fatal("back must not restart Docker"); return nil }, nil)
	if !errors.Is(err, ui.ErrReturnToMenu) {
		t.Fatal(err)
	}
	if next, err := view.Ask("parent"); err != nil || next != "parent-next" {
		t.Fatalf("overwrite skipped a page: next=%q err=%v", next, err)
	}
	if storage.ReadFileString(path) != original {
		t.Fatal("back wrote the daemon configuration")
	}
	assertNoBackups(t, path)
}

func navigationUI(t *testing.T, input string) *ui.UI {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "input")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	if _, err := f.WriteString(input); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	previous := os.Stdin
	os.Stdin = f
	view := ui.New()
	os.Stdin = previous
	return view
}
