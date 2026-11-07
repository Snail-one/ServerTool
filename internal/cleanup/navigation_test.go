package cleanup

import (
	"errors"
	"os"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
	"testing"
)

func TestCleanupResultReturnsToCleanupSelection(t *testing.T) {
	view := navigationUI(t, "1\ny\n\nq\nparent-next\n")
	calls := 0
	steps := []cleanupStep{{label: "测试配置", run: func(*system.Account) error { calls++; return nil }}}
	err := runMenu(view, &system.Account{Name: "test"}, steps)
	if !errors.Is(err, shared.ErrReturnToMenu) {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("cleanup ran %d times", calls)
	}
	if next, err := view.Ask("parent"); err != nil || next != "parent-next" {
		t.Fatalf("cleanup result skipped menu: next=%q err=%v", next, err)
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
