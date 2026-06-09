package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplaceAliasesAddsManagedBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bashrc")
	if err := os.WriteFile(path, []byte("export PATH=$PATH:/usr/local/bin\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := replaceAliases(path); err != nil {
		t.Fatal(err)
	}

	content := readTestFile(t, path)
	if !strings.Contains(content, bashAliasBegin) || !strings.Contains(content, bashAliasEnd) {
		t.Fatalf("managed alias block was not written:\n%s", content)
	}
	for _, line := range strings.Split(bashAliasBlock, "\n") {
		if !strings.Contains(content, line) {
			t.Fatalf("missing alias %q in:\n%s", line, content)
		}
	}
}

func TestReplaceAliasesReplacesLegacyAliasesAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bashrc")
	input := `alias ll='ls $LS_OPTIONS -l'
alias la='ls -A'
alias l='ls -la'
export EDITOR=vim
`
	if err := os.WriteFile(path, []byte(input), 0644); err != nil {
		t.Fatal(err)
	}

	if err := replaceAliases(path); err != nil {
		t.Fatal(err)
	}
	if err := replaceAliases(path); err != nil {
		t.Fatal(err)
	}

	content := readTestFile(t, path)
	if strings.Contains(content, "ls $LS_OPTIONS") || strings.Contains(content, "alias l='ls -la'") {
		t.Fatalf("legacy aliases were not removed:\n%s", content)
	}
	if strings.Count(content, bashAliasBegin) != 1 || strings.Count(content, "alias ll='ls -l'") != 1 {
		t.Fatalf("alias block is not idempotent:\n%s", content)
	}
	if !strings.Contains(content, "export EDITOR=vim") {
		t.Fatalf("unrelated bashrc content was removed:\n%s", content)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
