package clientproxy

import (
	"errors"
	"os"
	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
	"testing"
)

func TestProxyOverwriteBackReturnsToAddress(t *testing.T) {
	account, path := setup(t)
	original := `{"proxies":{"default":{"httpProxy":"http://keep:8080"}}}`
	putFile(t, path, original)
	view := navigationUI(t, "192.0.2.1:8080\nq\nq\nparent-next\n")
	if err := configure(view, account); !errors.Is(err, shared.ErrReturnToMenu) {
		t.Fatal(err)
	}
	if next, err := view.Ask("parent"); err != nil || next != "parent-next" {
		t.Fatalf("proxy skipped address: next=%q err=%v", next, err)
	}
	if shared.ReadFileString(path) != original {
		t.Fatal("back changed client configuration")
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
