package cleanup

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

func TestCleanupResultReturnsToCleanupSelection(t *testing.T) {
	view := navigationUI(t, "1\ny\n\nq\nparent-next\n")
	calls := 0
	steps := []cleanupStep{{label: "测试配置", run: func(*system.Account) error { calls++; return nil }}}
	err := runMenu(view, &system.Account{Name: "test"}, steps)
	if !errors.Is(err, ui.ErrReturnToMenu) {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("cleanup ran %d times", calls)
	}
	if next, err := view.Ask("parent"); err != nil || next != "parent-next" {
		t.Fatalf("cleanup result skipped menu: next=%q err=%v", next, err)
	}
}

func TestVSCodeServerUninstallFromCleanupMenu(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{".vscode-server", ".vscode-server-insiders", ".vscode-remote", ".vscode"} {
		if err := os.MkdirAll(filepath.Join(home, name, "extensions"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	account := &system.Account{Name: "test", Home: home, UID: os.Getuid()}
	for _, answer := range []string{"n\n", "y\n\n"} {
		view := navigationUI(t, "15\n"+answer+"q\nparent-next\n")
		if err := runMenu(view, account, allCleanupSteps()); !errors.Is(err, ui.ErrReturnToMenu) {
			t.Fatal(err)
		}
		if next, err := view.Ask("parent"); err != nil || next != "parent-next" {
			t.Fatalf("uninstall did not return through the cleanup menu: next=%q err=%v", next, err)
		}
		for _, name := range []string{".vscode-server", ".vscode-server-insiders", ".vscode-remote"} {
			_, err := os.Stat(filepath.Join(home, name))
			if answer == "n\n" && err != nil || answer != "n\n" && !os.IsNotExist(err) {
				t.Fatalf("unexpected state after %q for %s: %v", answer, name, err)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".vscode", "extensions")); err != nil {
		t.Fatal(err)
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
