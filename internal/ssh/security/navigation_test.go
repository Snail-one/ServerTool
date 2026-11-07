package security

import (
	"errors"
	"os"
	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
	"testing"
)

func TestRootLoginConfirmationBackReturnsToPolicyChoice(t *testing.T) {
	for _, choice := range []string{"1", "2"} {
		view := navigationUI(t, choice+"\nq\nq\nparent-next\n")
		_, err := promptRootLoginPolicy(view, parseSSHDSettings("permitrootlogin prohibit-password"))
		if !errors.Is(err, shared.ErrReturnToMenu) {
			t.Fatal(err)
		}
		if next, err := view.Ask("parent"); err != nil || next != "parent-next" {
			t.Fatalf("policy back skipped choice: next=%q err=%v", next, err)
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
