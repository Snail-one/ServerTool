package vim

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"snail_tool/internal/shared"
)

func TestReplaceVimConfigPreservesManualConfigAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".vimrc")
	manual := "set number\ncolorscheme desert\n"
	if err := os.WriteFile(path, []byte(manual), 0600); err != nil {
		t.Fatal(err)
	}

	if err := replaceVimConfig(path); err != nil {
		t.Fatal(err)
	}
	first := readTestVimFile(t, path)
	wantBlock := shared.FormatManagedBlock(vimrcBegin, vimrcContent, vimrcEnd)
	if !strings.HasPrefix(first, manual) {
		t.Fatalf("manual Vim configuration was changed:\n%s", first)
	}
	if !strings.Contains(first, wantBlock) {
		t.Fatalf("managed Vim block was not written:\n%s", first)
	}
	if strings.Count(first, vimrcBegin) != 1 || strings.Count(first, vimrcEnd) != 1 {
		t.Fatalf("managed Vim markers were duplicated:\n%s", first)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0600 {
		t.Fatalf("file mode = %o, want 600", info.Mode().Perm())
	}

	if err := replaceVimConfig(path); err != nil {
		t.Fatal(err)
	}
	if second := readTestVimFile(t, path); second != first {
		t.Fatalf("second write changed Vim configuration:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestReplaceVimConfigMigratesLegacyWholeFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".vimrc")
	if err := os.WriteFile(path, []byte(legacyVimrcContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := replaceVimConfig(path); err != nil {
		t.Fatal(err)
	}
	got := readTestVimFile(t, path)
	want := shared.FormatManagedBlock(vimrcBegin, vimrcContent, vimrcEnd)
	if got != want {
		t.Fatalf("legacy Vim configuration was not migrated:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestIsManagedVimConfigContentAllowsManualConfigOutsideBlock(t *testing.T) {
	content := "set number\n\n" + shared.FormatManagedBlock(vimrcBegin, vimrcContent, vimrcEnd)
	if !IsManagedVimConfigContent(content) {
		t.Fatal("managed Vim block with surrounding manual config was not detected")
	}

	modified := strings.Replace(content, "set mouse=", "set mouse=a", 1)
	if IsManagedVimConfigContent(modified) {
		t.Fatal("modified managed Vim block should not be reported as configured")
	}
}

func readTestVimFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
