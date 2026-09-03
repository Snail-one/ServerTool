package bash

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

const (
	bashAliasBegin = "# ===== BEGIN SNAIL BASH ALIASES ====="
	bashAliasEnd   = "# ===== END SNAIL BASH ALIASES ====="
	baseAliasBlock = `alias l='ls -lh'
alias la='ls -A'
alias ll='ls -lah'
alias lspath='echo "$PATH" | tr ":" "\n"'`
	grepColorAlias            = `alias grep='grep --color=auto'`
	bashAliasBlock            = baseAliasBlock + "\n" + grepColorAlias
	rootInteractiveShellBlock = `# If not running interactively, don't do anything
case $- in
    *i*) ;;
      *) return;;
esac`
	rootHistoryBlock = `# -------------------------
# History
# -------------------------

# Ignore duplicate commands
HISTCONTROL=ignoredups

# Append history instead of overwriting it
shopt -s histappend

# History size
HISTSIZE=5000
HISTFILESIZE=10000`
	rootShellBehaviorBlock = `# -------------------------
# Shell behavior
# -------------------------

# Update LINES and COLUMNS after terminal resize
shopt -s checkwinsize

# Uncomment if you want ** to recursively match directories
# shopt -s globstar`
	userPromptBlock = `# -------------------------
# Prompt
# -------------------------

# user@hostname = purple
# current directory = blue
case "$TERM" in
    xterm*|screen*|tmux*|*-256color|linux) color_prompt=yes;;
esac

if [ "$color_prompt" = yes ]; then
    PS1='${debian_chroot:+($debian_chroot)}\[\033[01;35m\]\u@\h\[\033[00m\]:\[\033[01;34m\]\w\[\033[00m\]\$ '
else
    PS1='${debian_chroot:+($debian_chroot)}\u@\h:\w\$ '
fi
unset color_prompt force_color_prompt`
	rootPromptBlock = `# -------------------------
# Debian chroot support
# -------------------------

if [ -z "${debian_chroot:-}" ] && [ -r /etc/debian_chroot ]; then
    debian_chroot=$(cat /etc/debian_chroot)
fi

# -------------------------
# Prompt
# -------------------------

# root@hostname = pure orange (#FF7F00)
# current directory = blue
case "$TERM" in
    xterm*|screen*|tmux*|*-256color|linux) color_prompt=yes;;
esac

if [ "$color_prompt" = yes ]; then
    PS1='${debian_chroot:+($debian_chroot)}\[\033[38;2;255;127;0m\]\u@\h\[\033[00m\]:\[\033[01;34m\]\w\[\033[00m\]\$ '
else
    PS1='${debian_chroot:+($debian_chroot)}\u@\h:\w\$ '
fi
unset color_prompt force_color_prompt`
	rootDircolorsBlock = `if command -v dircolors >/dev/null 2>&1; then
    eval "$(dircolors -b)"
fi`
	rootColorAliasBlock = `alias ls='ls --color=auto'` + "\n" + grepColorAlias
	rootColorBlock      = `# -------------------------
# Colors
# -------------------------

` + rootDircolorsBlock + "\n" + rootColorAliasBlock
	rootSafetyAliasBlock = `# Some more alias to avoid making mistakes:
alias rm='rm -i'
alias cp='cp -i'
alias mv='mv -i'`
	userBashBlock = bashAliasBlock + "\n\n" + userPromptBlock
	rootBashBlock = rootInteractiveShellBlock + "\n\n" + rootHistoryBlock + "\n\n" + rootShellBehaviorBlock + "\n\n" + rootPromptBlock + "\n\n" + rootColorBlock + "\n\n" + baseAliasBlock + "\n\n" + rootSafetyAliasBlock
)

func BashAliasMarkers() (string, string) {
	return bashAliasBegin, bashAliasEnd
}

func BashAliasBlock() string {
	return bashAliasBlock
}

// BashManagedBlock returns the default managed block written for a regular user.
func BashManagedBlock() string {
	return userBashBlock
}

func IsBashConfigured(account *system.Account) bool {
	bashrc := filepath.Join(account.Home, ".bashrc")
	content := shared.ReadFileString(bashrc)
	managed, ok := shared.ManagedBlockContent(content, bashAliasBegin, bashAliasEnd)
	if !ok {
		return false
	}
	expected := userBashBlock
	if isRootAccount(account) {
		expected = rootBashBlock
	}
	return strings.TrimSpace(managed) == strings.TrimSpace(expected)
}

func isRootAccount(account *system.Account) bool {
	return account != nil && account.Name == "root"
}

func Run() error {
	return ConfigureBash()
}

func ConfigureBash() error {
	account, err := system.CurrentTargetUser()
	if err != nil {
		return err
	}

	bashrc := filepath.Join(account.Home, ".bashrc")
	log.Info("配置 Bash 环境...")
	ui.PrintInfoCard("Bash 配置信息",
		ui.CardField{Label: "当前用户", Value: account.Name},
		ui.CardField{Label: "配置文件", Value: bashrc},
	)

	if err := shared.EnsureFileWithOptions(bashrc, shared.AtomicWriteOptions{
		Mode: 0644, Owner: &shared.FileOwner{UID: account.UID, GID: account.GID},
	}); err != nil {
		return err
	}
	bashBlock := userBashBlock
	if isRootAccount(account) {
		bashBlock = rootBashBlock
	}
	if err := replaceBashConfig(bashrc, bashBlock); err != nil {
		return err
	}

	if err := system.ChownPath(bashrc, account, false); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println(ui.PrimaryBoldText("已经写入以下 Bash 配置："))
	fmt.Println(bashBlock)
	fmt.Println()
	fields := []ui.CardField{
		{Label: "配置文件", Value: bashrc},
		{Label: "立即生效", Value: "source " + bashrc},
	}
	if !isRootAccount(account) {
		fields = append(fields, ui.CardField{Label: "终端提示符", Value: "用户名@主机名为紫色，当前目录为蓝色"})
	}
	ui.PrintSuccessCard("Bash 配置完成", fields...)
	return nil
}

func replaceAliases(path string) error {
	return replaceBashConfig(path, userBashBlock)
}

func replaceBashConfig(path, body string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	content := shared.RemoveManagedBlock(string(data), bashAliasBegin, bashAliasEnd)
	block := shared.FormatManagedBlock(bashAliasBegin, body, bashAliasEnd)
	return shared.AtomicWriteFile(path, []byte(shared.AppendBlock(content, block)), shared.AtomicWriteOptions{Mode: 0644})
}
