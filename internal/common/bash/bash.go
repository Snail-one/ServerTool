package bash

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

const (
	bashAliasBegin = "# ===== BEGIN SNAIL BASH ALIASES ====="
	bashAliasEnd   = "# ===== END SNAIL BASH ALIASES ====="
	bashAliasBlock = `alias l='ls -lh'
alias la='ls -A'
alias ll='ls -lah'
alias lspath='echo "$PATH" | tr ":" "\n"'`
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
	rootPromptBlock = `# -------------------------
# Debian chroot support
# -------------------------

if [ -z "${debian_chroot:-}" ] && [ -r /etc/debian_chroot ]; then
    debian_chroot=$(cat /etc/debian_chroot)
fi

# -------------------------
# Prompt
# -------------------------

# root@hostname = red
# current directory = blue
PS1='${debian_chroot:+($debian_chroot)}\[\033[38;5;196m\]\u@\h\[\033[00m\]:\[\033[01;34m\]\w\[\033[00m\]\$ '

# PS1='\[\033[01;35m\]\u@\h\[\033[00m\]:\[\033[01;34m\]\w\[\033[00m\]\$ '`
	rootDircolorsBlock = `if command -v dircolors >/dev/null 2>&1; then
    eval "$(dircolors -b)"
fi`
	rootColorAliasBlock = `alias ls='ls --color=auto'
alias grep='grep --color=auto'
alias fgrep='fgrep --color=auto'
alias egrep='egrep --color=auto'`
	rootColorBlock = `# -------------------------
# Colors
# -------------------------

` + rootDircolorsBlock + "\n" + rootColorAliasBlock
	rootSafetyAliasBlock = `# Some more alias to avoid making mistakes:
alias rm='rm -i'
alias cp='cp -i'
alias mv='mv -i'`
	rootBashBlock = rootHistoryBlock + "\n\n" + rootShellBehaviorBlock + "\n\n" + rootPromptBlock + "\n\n" + rootColorBlock + "\n\n" + bashAliasBlock + "\n\n" + rootSafetyAliasBlock
)

var (
	activeAssignment = regexp.MustCompile(`^(?:export[ \t]+)?([A-Za-z_][A-Za-z0-9_]*)[ \t]*=`)
	activeAlias      = regexp.MustCompile(`^alias[ \t]+([A-Za-z_][A-Za-z0-9_]*)[ \t]*=`)
)

func BashAliasMarkers() (string, string) {
	return bashAliasBegin, bashAliasEnd
}

func BashAliasBlock() string {
	return bashAliasBlock
}

func IsBashConfigured(account *system.Account) bool {
	bashrc := filepath.Join(account.Home, ".bashrc")
	content := shared.ReadFileString(bashrc)
	managed, ok := shared.ManagedBlockContent(content, bashAliasBegin, bashAliasEnd)
	if !ok || !strings.Contains(managed, bashAliasBlock) {
		return false
	}
	return !isRootAccount(account) || strings.Contains(managed, rootSafetyAliasBlock)
}

func isRootAccount(account *system.Account) bool {
	return account != nil && account.Name == "root"
}

func rootBashBlockForContent(content string) (string, bool, bool) {
	preservePrompt := hasActiveAssignment(content, "PS1")
	colorBlock, preserveColor := rootColorBlockForContent(content)

	parts := make([]string, 0, 6)
	if historyBlock := rootHistoryBlockForContent(content); historyBlock != "" {
		parts = append(parts, historyBlock)
	}
	if !hasActiveShopt(content, "checkwinsize") {
		parts = append(parts, rootShellBehaviorBlock)
	}
	if !preservePrompt {
		parts = append(parts, rootPromptBlock)
	}
	if colorBlock != "" {
		parts = append(parts, colorBlock)
	}
	parts = append(parts, bashAliasBlock, rootSafetyAliasBlock)
	return strings.Join(parts, "\n\n"), preservePrompt, preserveColor
}

func activeShellLines(content string) []string {
	lines := make([]string, 0)
	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(rawLine)
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return lines
}

func hasActiveAssignment(content, name string) bool {
	for _, line := range activeShellLines(content) {
		match := activeAssignment.FindStringSubmatch(line)
		if match != nil && match[1] == name {
			return true
		}
	}
	return false
}

func hasActiveAlias(content, name string) bool {
	for _, line := range activeShellLines(content) {
		match := activeAlias.FindStringSubmatch(line)
		if match != nil && match[1] == name {
			return true
		}
	}
	return false
}

func hasActiveShopt(content, option string) bool {
	for _, line := range activeShellLines(content) {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "shopt" || fields[1] != "-s" {
			continue
		}
		for _, field := range fields[2:] {
			if field == option {
				return true
			}
		}
	}
	return false
}

func rootHistoryBlockForContent(content string) string {
	lines := make([]string, 0, 4)
	if !hasActiveAssignment(content, "HISTCONTROL") {
		lines = append(lines, "HISTCONTROL=ignoredups")
	}
	if !hasActiveShopt(content, "histappend") {
		lines = append(lines, "shopt -s histappend")
	}
	if !hasActiveAssignment(content, "HISTSIZE") {
		lines = append(lines, "HISTSIZE=5000")
	}
	if !hasActiveAssignment(content, "HISTFILESIZE") {
		lines = append(lines, "HISTFILESIZE=10000")
	}
	if len(lines) == 0 {
		return ""
	}
	if len(lines) == 4 {
		return rootHistoryBlock
	}
	return "# -------------------------\n# History\n# -------------------------\n\n" + strings.Join(lines, "\n")
}

func rootColorBlockForContent(content string) (string, bool) {
	hasDircolors := hasActiveAssignment(content, "LS_OPTIONS") || hasActiveAssignment(content, "LS_COLORS")
	if !hasDircolors {
		for _, line := range activeShellLines(content) {
			if strings.Contains(line, "eval") && strings.Contains(line, "dircolors") {
				hasDircolors = true
				break
			}
		}
	}

	parts := make([]string, 0, 5)
	if !hasDircolors {
		parts = append(parts, rootDircolorsBlock)
	}
	preserved := hasDircolors
	for _, item := range []struct {
		name string
		line string
	}{
		{name: "ls", line: "alias ls='ls --color=auto'"},
		{name: "grep", line: "alias grep='grep --color=auto'"},
		{name: "fgrep", line: "alias fgrep='fgrep --color=auto'"},
		{name: "egrep", line: "alias egrep='egrep --color=auto'"},
	} {
		if hasActiveAlias(content, item.name) {
			preserved = true
			continue
		}
		parts = append(parts, item.line)
	}
	if len(parts) == 0 {
		return "", preserved
	}
	return "# -------------------------\n# Colors\n# -------------------------\n\n" + strings.Join(parts, "\n"), preserved
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
	bashBlock := bashAliasBlock
	preservedPrompt := false
	preservedColor := false
	if isRootAccount(account) {
		data, err := os.ReadFile(bashrc)
		if err != nil {
			return err
		}
		content := string(data)
		unmanaged := shared.RemoveManagedBlock(content, bashAliasBegin, bashAliasEnd)
		bashBlock, preservedPrompt, preservedColor = rootBashBlockForContent(unmanaged)
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
	if preservedPrompt {
		fmt.Println("保留了已有的 PS1 配置")
	}
	if preservedColor {
		fmt.Println("保留了已有的颜色配置")
	}
	fmt.Println()
	fields := []ui.CardField{
		{Label: "配置文件", Value: bashrc},
		{Label: "立即生效", Value: "source " + bashrc},
	}
	if preservedPrompt {
		fields = append(fields, ui.CardField{Label: "已有 PS1", Value: "已保留"})
	}
	if preservedColor {
		fields = append(fields, ui.CardField{Label: "已有颜色配置", Value: "已保留"})
	}
	ui.PrintSuccessCard("Bash 配置完成", fields...)
	return nil
}

func replaceAliases(path string) error {
	return replaceBashConfig(path, bashAliasBlock)
}

func replaceBashConfig(path, body string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	content := shared.RemoveManagedBlock(string(data), bashAliasBegin, bashAliasEnd)
	patterns := []string{
		`(?m)^[[:space:]]*#?[[:space:]]*alias lspath=.*\n?`,
		`(?m)^[[:space:]]*#?[[:space:]]*alias ll=.*\n?`,
		`(?m)^[[:space:]]*#?[[:space:]]*alias la=.*\n?`,
		`(?m)^[[:space:]]*#?[[:space:]]*alias l=.*\n?`,
	}
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		content = re.ReplaceAllString(content, "")
	}

	block := shared.FormatManagedBlock(bashAliasBegin, body, bashAliasEnd)
	return shared.AtomicWriteFile(path, []byte(shared.AppendBlock(content, block)), shared.AtomicWriteOptions{Mode: 0644})
}
