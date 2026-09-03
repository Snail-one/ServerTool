package bash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"snail_tool/internal/shared"
	"snail_tool/internal/system"
)

func TestReplaceAliasesPreservesManualConfigAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bashrc")
	manual := `alias l='eza'
alias la='eza -a'
alias ll='eza -la'
alias lspath='printf custom-path'
alias grep='grep --color=always'
alias ls='eza --color=always'
alias rm='rm --verbose'
PS1='custom prompt '
HISTSIZE=20000
export LS_COLORS='custom colors'
`
	if err := os.WriteFile(path, []byte(manual), 0600); err != nil {
		t.Fatal(err)
	}

	if err := replaceAliases(path); err != nil {
		t.Fatal(err)
	}
	first := readTestFile(t, path)
	if !strings.HasPrefix(first, manual) {
		t.Fatalf("manual Bash configuration was changed:\n%s", first)
	}
	wantBlock := shared.FormatManagedBlock(bashAliasBegin, userBashBlock, bashAliasEnd)
	if !strings.Contains(first, wantBlock) {
		t.Fatalf("complete managed Bash block was not written:\n%s", first)
	}
	if !strings.HasSuffix(first, wantBlock) {
		t.Fatalf("managed Bash block was not appended after manual configuration:\n%s", first)
	}
	if strings.Count(first, bashAliasBegin) != 1 || strings.Count(first, bashAliasEnd) != 1 {
		t.Fatalf("managed Bash markers were duplicated:\n%s", first)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0600 {
		t.Fatalf("file mode = %o, want 600", info.Mode().Perm())
	}

	if err := replaceAliases(path); err != nil {
		t.Fatal(err)
	}
	if second := readTestFile(t, path); second != first {
		t.Fatalf("second write changed Bash configuration:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestReplaceBashConfigReplacesOnlyManagedBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bashrc")
	manual := "alias ll='my-list-command'\nPS1='my prompt ' # restored after cleanup\n"
	legacyBody := "alias ll='legacy-managed-value'\n"
	existing := manual + "\n" + shared.FormatManagedBlock(bashAliasBegin, legacyBody, bashAliasEnd)
	if err := os.WriteFile(path, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if err := replaceBashConfig(path, userBashBlock); err != nil {
		t.Fatal(err)
	}
	content := readTestFile(t, path)
	if !strings.HasPrefix(content, manual) {
		t.Fatalf("configuration outside the managed block was changed:\n%s", content)
	}
	if strings.Contains(content, "legacy-managed-value") {
		t.Fatalf("old managed Bash content remained:\n%s", content)
	}
	if strings.Count(content, "alias ll='my-list-command'") != 1 {
		t.Fatalf("manual ll alias was removed or duplicated:\n%s", content)
	}
	if strings.Count(content, bashAliasBegin) != 1 || strings.Count(content, bashAliasEnd) != 1 {
		t.Fatalf("managed Bash block was duplicated:\n%s", content)
	}
}

func TestManagedBlockUsesFixedTemplateDespiteExistingConfig(t *testing.T) {
	manual := `alias grep='grep --color=always'
alias ls='eza'
PS1='custom prompt '
HISTCONTROL=erasedups
HISTSIZE=20000
HISTFILESIZE=40000
export LS_COLORS='custom colors'
eval "$(dircolors -b ~/.dircolors)"
`

	for _, tt := range []struct {
		name string
		body string
	}{
		{name: "regular user", body: userBashBlock},
		{name: "root", body: rootBashBlock},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".bashrc")
			if err := os.WriteFile(path, []byte(manual), 0644); err != nil {
				t.Fatal(err)
			}
			if err := replaceBashConfig(path, tt.body); err != nil {
				t.Fatal(err)
			}

			content := readTestFile(t, path)
			if !strings.HasPrefix(content, manual) {
				t.Fatalf("manual configuration was changed:\n%s", content)
			}
			managed, ok := shared.ManagedBlockContent(content, bashAliasBegin, bashAliasEnd)
			if !ok || strings.TrimSpace(managed) != strings.TrimSpace(tt.body) {
				t.Fatalf("managed block depends on manual configuration:\n%s", content)
			}
		})
	}
}

func TestBashConfigurationStatusRequiresCurrentCompleteTemplate(t *testing.T) {
	for _, tt := range []struct {
		name    string
		account *system.Account
		body    string
	}{
		{name: "regular user", account: &system.Account{Name: "alice", UID: 1000, GID: 1000}, body: userBashBlock},
		{name: "root", account: &system.Account{Name: "root"}, body: rootBashBlock},
	} {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			tt.account.Home = home
			path := filepath.Join(home, ".bashrc")
			manual := "alias ll='custom value'\n\n"
			current := manual + shared.FormatManagedBlock(bashAliasBegin, tt.body, bashAliasEnd)
			if err := os.WriteFile(path, []byte(current), 0644); err != nil {
				t.Fatal(err)
			}
			if !IsBashConfigured(tt.account) {
				t.Fatal("current complete managed Bash template was not detected")
			}

			modified := strings.Replace(current, "alias l='ls -lh'", "alias l='changed'", 1)
			if err := os.WriteFile(path, []byte(modified), 0644); err != nil {
				t.Fatal(err)
			}
			if IsBashConfigured(tt.account) {
				t.Fatal("modified managed Bash template was reported as configured")
			}
		})
	}
}

func TestNonRootAccountUsesRegularUserTemplate(t *testing.T) {
	account := &system.Account{Name: "alice", UID: 1000, GID: 1000}
	if isRootAccount(account) {
		t.Fatal("non-root account was treated as root")
	}
	for _, required := range []string{bashAliasBlock, userPromptBlock} {
		if !strings.Contains(BashManagedBlock(), required) {
			t.Fatalf("regular user Bash configuration is missing %q:\n%s", required, BashManagedBlock())
		}
	}
}

func TestUserPromptUsesPurpleUsernameAndBlueDirectory(t *testing.T) {
	for _, required := range []string{
		`case "$TERM" in`,
		`xterm*|screen*|tmux*|*-256color|linux) color_prompt=yes;;`,
		`PS1='${debian_chroot:+($debian_chroot)}\[\033[01;35m\]\u@\h\[\033[00m\]:\[\033[01;34m\]\w\[\033[00m\]\$ '`,
		`PS1='${debian_chroot:+($debian_chroot)}\u@\h:\w\$ '`,
		`unset color_prompt force_color_prompt`,
	} {
		if !strings.Contains(userPromptBlock, required) {
			t.Fatalf("regular user prompt is missing %q:\n%s", required, userPromptBlock)
		}
	}
}

func TestRootManagedBlockContainsCompleteConfiguration(t *testing.T) {
	if !strings.HasPrefix(rootBashBlock, rootInteractiveShellBlock+"\n\n") {
		t.Fatalf("root Bash configuration does not start with the interactive shell guard:\n%s", rootBashBlock)
	}
	for _, required := range []string{
		rootHistoryBlock,
		rootShellBehaviorBlock,
		rootPromptBlock,
		rootColorBlock,
		baseAliasBlock,
		rootSafetyAliasBlock,
	} {
		if !strings.Contains(rootBashBlock, required) {
			t.Fatalf("root Bash configuration is missing %q:\n%s", required, rootBashBlock)
		}
	}
	for _, deprecated := range []string{"alias fgrep=", "alias egrep="} {
		if strings.Contains(rootBashBlock, deprecated) {
			t.Fatalf("root color configuration contains deprecated alias %q:\n%s", deprecated, rootBashBlock)
		}
	}
}

func TestRootPromptUsesColorCapabilityFallback(t *testing.T) {
	for _, required := range []string{
		`case "$TERM" in`,
		`xterm*|screen*|tmux*|*-256color|linux) color_prompt=yes;;`,
		`if [ "$color_prompt" = yes ]; then`,
		`PS1='${debian_chroot:+($debian_chroot)}\[\033[38;2;255;127;0m\]\u@\h\[\033[00m\]:\[\033[01;34m\]\w\[\033[00m\]\$ '`,
		`PS1='${debian_chroot:+($debian_chroot)}\u@\h:\w\$ '`,
		`unset color_prompt force_color_prompt`,
	} {
		if !strings.Contains(rootPromptBlock, required) {
			t.Fatalf("root prompt is missing %q:\n%s", required, rootPromptBlock)
		}
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
