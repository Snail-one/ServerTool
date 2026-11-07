package golang

import (
	"errors"
	"os"
	"path/filepath"
	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
	"testing"
)

func TestGoUninstallConfirmationBackReturnsToVersionSelection(t *testing.T) {
	for _, official := range []bool{false, true} {
		root := t.TempDir()
		writeManagedVersion(t, filepath.Join(root, "go1.24.1"), "keep")
		view := navigationUI(t, "1\nq\nq\nparent-next\n")
		_, _, _, err := promptGoUninstall(view, []string{"go1.24.1"}, official, root)
		if !errors.Is(err, shared.ErrReturnToMenu) {
			t.Fatal(err)
		}
		if next, err := view.Ask("parent"); err != nil || next != "parent-next" {
			t.Fatalf("uninstall skipped version selection: next=%q err=%v", next, err)
		}
		if got := readFile(t, filepath.Join(root, "go1.24.1", "bin", "go")); got != "keep" {
			t.Fatal("back deleted Go")
		}
	}
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
