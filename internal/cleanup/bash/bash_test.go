package bash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"snail_tool/internal/system"
)

func TestRunRemovesManagedBlocksOnly(t *testing.T) {
	home := t.TempDir()
	account := &system.Account{Name: "test", Home: home}
	bashrc := filepath.Join(home, ".bashrc")
	content := "export EDITOR=vim\n\n" +
		bashAliasBegin + "\n" + bashAliasBlock + "\n" + bashAliasEnd + "\n\n" +
		legacyBashCommandBegin + "\nsnail() {\n  sudo '/usr/local/bin/snail_tool' \"$@\"\n}\n" + legacyBashCommandEnd + "\n"
	if err := os.WriteFile(bashrc, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Run(account); err != nil {
		t.Fatal(err)
	}

	got := readTestFile(t, bashrc)
	for _, marker := range []string{bashAliasBegin, bashAliasEnd, legacyBashCommandBegin, legacyBashCommandEnd, "snail()"} {
		if strings.Contains(got, marker) {
			t.Fatalf("managed bash content remained:\n%s", got)
		}
	}
	if !strings.Contains(got, "export EDITOR=vim") {
		t.Fatalf("unrelated bash content was removed:\n%s", got)
	}
}

func TestRunKeepsEmptyManagedFile(t *testing.T) {
	home := t.TempDir()
	account := &system.Account{Name: "test", Home: home}
	bashrc := filepath.Join(home, ".bashrc")
	content := bashAliasBegin + "\n" + bashAliasBlock + "\n" + bashAliasEnd + "\n"
	if err := os.WriteFile(bashrc, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Run(account); err != nil {
		t.Fatal(err)
	}

	if got := readTestFile(t, bashrc); got != "" {
		t.Fatalf("expected managed .bashrc to be kept empty, got:\n%s", got)
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
