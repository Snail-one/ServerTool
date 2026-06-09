package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"snail_tool/internal/system"
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

func TestWriteSnailCommandIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bashrc")
	if err := os.WriteFile(path, []byte("export EDITOR=vim\n"), 0644); err != nil {
		t.Fatal(err)
	}

	block := `snail() {
  sudo '/usr/local/bin/snail_tool' "$@"
}`
	if err := writeSnailCommand(path, block); err != nil {
		t.Fatal(err)
	}
	if err := writeSnailCommand(path, block); err != nil {
		t.Fatal(err)
	}

	content := readTestFile(t, path)
	if strings.Count(content, bashCommandBegin) != 1 || strings.Count(content, "snail()") != 1 {
		t.Fatalf("snail command block is not idempotent:\n%s", content)
	}
	if !strings.Contains(content, "export EDITOR=vim") {
		t.Fatalf("unrelated bashrc content was removed:\n%s", content)
	}
}

func TestBuildSnailCommandBlock(t *testing.T) {
	userBlock, err := buildSnailCommandBlock(&system.Account{Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(userBlock, "sudo ") || !strings.Contains(userBlock, "snail()") {
		t.Fatalf("expected non-root block to use sudo:\n%s", userBlock)
	}

	rootBlock, err := buildSnailCommandBlock(&system.Account{Name: "root"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rootBlock, "sudo ") {
		t.Fatalf("expected root block to avoid sudo:\n%s", rootBlock)
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
