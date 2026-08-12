package bash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"snail_tool/internal/shared"
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
	if want := shared.FormatManagedBlock(bashAliasBegin, bashAliasBlock, bashAliasEnd); !strings.Contains(content, want) {
		t.Fatalf("managed alias block spacing mismatch:\n%s", content)
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
	if strings.Count(content, bashAliasBegin) != 1 || strings.Count(content, bashAliasEnd) != 1 {
		t.Fatalf("alias block is not idempotent:\n%s", content)
	}
	for _, line := range strings.Split(bashAliasBlock, "\n") {
		if strings.Count(content, line) != 1 {
			t.Fatalf("alias line %q is not idempotent:\n%s", line, content)
		}
	}
	if !strings.Contains(content, "export EDITOR=vim") {
		t.Fatalf("unrelated bashrc content was removed:\n%s", content)
	}
}

func TestRootBashConfigWritesRequestedManagedBlockAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bashrc")
	if err := os.WriteFile(path, []byte("# existing root setting\n"), 0644); err != nil {
		t.Fatal(err)
	}

	account := &system.Account{Name: "root", Home: filepath.Dir(path), UID: 0, GID: 0}
	body := bashBlockForAccount(account)
	if body != rootBashBlock {
		t.Fatal("root account did not select the root Bash configuration")
	}
	if err := replaceBashConfig(path, body); err != nil {
		t.Fatal(err)
	}
	if err := replaceBashConfig(path, body); err != nil {
		t.Fatal(err)
	}

	content := readTestFile(t, path)
	want := shared.FormatManagedBlock(bashAliasBegin, rootBashBlock, bashAliasEnd)
	if !strings.Contains(content, want) {
		t.Fatalf("root Bash configuration was not written:\n%s", content)
	}
	if strings.Count(content, bashAliasBegin) != 1 || strings.Count(content, bashAliasEnd) != 1 {
		t.Fatalf("root Bash configuration is not idempotent:\n%s", content)
	}
	if !strings.Contains(content, "# existing root setting") {
		t.Fatalf("existing root Bash content was removed:\n%s", content)
	}
}

func TestNonRootAccountKeepsStandardAliasBlock(t *testing.T) {
	account := &system.Account{Name: "alice", UID: 1000, GID: 1000}
	if got := bashBlockForAccount(account); got != bashAliasBlock {
		t.Fatalf("non-root account selected unexpected Bash configuration:\n%s", got)
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
