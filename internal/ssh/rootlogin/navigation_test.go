package rootlogin

import (
	"errors"
	"os"
	"testing"

	"snail_tool/internal/ui"
)

func TestRootLoginConfirmationBackReturnsToPolicyChoice(t *testing.T) {
	for _, choice := range []string{"1", "2"} {
		for _, answer := range []string{"q", "n", ""} {
			view := navigationUI(t, choice+"\n"+answer+"\nq\nparent-next\n")
			err := Run(view, func() (string, error) { return "仅允许密钥登录", nil }, func(string) error {
				t.Fatal("cancelled confirmation changed the Root login policy")
				return nil
			})
			if !errors.Is(err, ui.ErrReturnToMenu) {
				t.Fatal(err)
			}
			if next, err := view.Ask("parent"); err != nil || next != "parent-next" {
				t.Fatalf("policy back skipped choice: next=%q err=%v", next, err)
			}
		}
	}
}

func TestRootLoginUpdateRefreshesPolicyMenu(t *testing.T) {
	view := navigationUI(t, "2\ny\n\nq\nparent-next\n")
	value := "仅允许密钥登录"
	reads := 0
	err := Run(view, func() (string, error) {
		reads++
		return value, nil
	}, func(policy string) error {
		if policy != "no" {
			t.Fatalf("unexpected policy: %s", policy)
		}
		value = "已禁止"
		return nil
	})
	if !errors.Is(err, ui.ErrReturnToMenu) || reads != 2 || value != "已禁止" {
		t.Fatalf("unexpected result: reads=%d value=%q err=%v", reads, value, err)
	}
	if next, err := view.Ask("parent"); err != nil || next != "parent-next" {
		t.Fatalf("update skipped policy menu: next=%q err=%v", next, err)
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
