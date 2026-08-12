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

	body, preservedPrompt, preservedColor := rootBashBlockForContent("# existing root setting\n")
	if body != rootBashBlock {
		t.Fatal("root account did not build the default root Bash configuration")
	}
	if preservedPrompt || preservedColor {
		t.Fatal("comment-only root configuration was treated as active")
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
	for _, alias := range strings.Split(bashAliasBlock, "\n") {
		if !strings.Contains(rootBashBlock, alias) {
			t.Fatalf("root Bash configuration is missing standard alias %q", alias)
		}
	}
	for _, oldAlias := range []string{"alias ll='ls $LS_OPTIONS -l'", "alias l='ls $LS_OPTIONS -lA'"} {
		if strings.Contains(rootBashBlock, oldAlias) {
			t.Fatalf("root Bash configuration still contains old alias %q", oldAlias)
		}
	}
}

func TestNonRootAccountKeepsStandardAliasBlock(t *testing.T) {
	account := &system.Account{Name: "alice", UID: 1000, GID: 1000}
	if isRootAccount(account) {
		t.Fatal("non-root account was treated as root")
	}
}

func TestRootBashConfigPreservesExistingPromptAndColorConfig(t *testing.T) {
	existing := `# PS1='commented prompt'
PS1='custom prompt '
export LS_COLORS='custom colors'
eval "$(dircolors -b ~/.dircolors)"
alias ls='ls --color=auto'
`

	body, preservedPrompt, preservedColor := rootBashBlockForContent(existing)
	if !preservedPrompt || !preservedColor {
		t.Fatalf("existing root settings were not detected: prompt=%v color=%v", preservedPrompt, preservedColor)
	}
	if strings.Contains(body, rootPromptBlock) || strings.Contains(body, rootColorBlock) {
		t.Fatalf("managed block duplicated existing prompt or color configuration:\n%s", body)
	}
	for _, required := range []string{bashAliasBlock, rootSafetyAliasBlock} {
		if !strings.Contains(body, required) {
			t.Fatalf("managed root configuration is missing required content %q:\n%s", required, body)
		}
	}
}

func TestRootBashConfigTreatsCommentedSettingsAsMissing(t *testing.T) {
	existing := `# PS1='commented prompt'
# export LS_OPTIONS='--color=auto'
# eval "$(dircolors)"
# alias ls='ls --color=auto'
`

	body, preservedPrompt, preservedColor := rootBashBlockForContent(existing)
	if preservedPrompt || preservedColor {
		t.Fatalf("commented settings were treated as active: prompt=%v color=%v", preservedPrompt, preservedColor)
	}
	if body != rootBashBlock {
		t.Fatalf("default root configuration was not generated:\n%s", body)
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
