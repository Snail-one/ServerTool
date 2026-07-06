package vim

import (
	"os"
	"path/filepath"
	"testing"

	"snail_tool/internal/system"
)

func TestRunClearsOnlyManagedTemplate(t *testing.T) {
	home := t.TempDir()
	account := &system.Account{Name: "test", Home: home}
	vimrc := filepath.Join(home, ".vimrc")

	if err := os.WriteFile(vimrc, []byte(vimrcContent), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Run(account); err != nil {
		t.Fatal(err)
	}
	if got := readTestFile(t, vimrc); got != "" {
		t.Fatalf("expected managed vimrc to be kept empty, got:\n%s", got)
	}

	if err := os.WriteFile(vimrc, []byte("set number\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Run(account); err != nil {
		t.Fatal(err)
	}
	if got := readTestFile(t, vimrc); got != "set number\n" {
		t.Fatalf("modified vimrc should be preserved, got:\n%s", got)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
