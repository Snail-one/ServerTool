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
)

const (
	bashAliasBegin = "# ===== BEGIN SNAIL BASH ALIASES ====="
	bashAliasEnd   = "# ===== END SNAIL BASH ALIASES ====="
	bashAliasBlock = `alias l='ls -lh'
alias la='ls -A'
alias ll='ls -lah'
alias lspath='echo "$PATH" | tr ":" "\n"'`
	rootPromptBlock = `# ~/.bashrc: executed by bash(1) for non-login shells.

# Note: PS1 is set in /etc/profile, and the default umask is defined
# in /etc/login.defs. You should not need this unless you want different
# defaults for root.

PS1='${debian_chroot:+($debian_chroot)}\[\033[38;5;196m\]\u@\h\[\033[00m\]:\[\033[01;34m\]\w\[\033[00m\]\$ '

# PS1='\[\033[01;35m\]\u@\h\[\033[00m\]:\[\033[01;34m\]\w\[\033[00m\]\$ '`
	rootColorBlock = `# umask 022
# You may uncomment the following lines if you want ` + "`" + `ls' to be colorized:
export LS_OPTIONS='--color=auto'
eval "$(dircolors)"
alias ls='ls $LS_OPTIONS'`
	rootSafetyAliasBlock = `# Some more alias to avoid making mistakes:
alias rm='rm -i'
alias cp='cp -i'
alias mv='mv -i'`
	rootBashBlock = rootPromptBlock + "\n\n" + rootColorBlock + "\n\n" + bashAliasBlock + "\n\n" + rootSafetyAliasBlock
)

var (
	activePS1Assignment = regexp.MustCompile(`^(?:export[ \t]+)?PS1[ \t]*=`)
	activeColorVariable = regexp.MustCompile(`^(?:export[ \t]+)?(?:LS_OPTIONS|LS_COLORS)[ \t]*=`)
	activeLSAlias       = regexp.MustCompile(`^alias[ \t]+ls[ \t]*=`)
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
	preservePrompt := hasActivePS1(content)
	preserveColor := hasActiveColorConfig(content)

	parts := make([]string, 0, 4)
	if !preservePrompt {
		parts = append(parts, rootPromptBlock)
	}
	if !preserveColor {
		parts = append(parts, rootColorBlock)
	}
	parts = append(parts, bashAliasBlock, rootSafetyAliasBlock)
	return strings.Join(parts, "\n\n"), preservePrompt, preserveColor
}

func hasActivePS1(content string) bool {
	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(rawLine)
		if line != "" && !strings.HasPrefix(line, "#") && activePS1Assignment.MatchString(line) {
			return true
		}
	}
	return false
}

func hasActiveColorConfig(content string) bool {
	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if activeColorVariable.MatchString(line) || activeLSAlias.MatchString(line) || strings.Contains(line, "dircolors") {
			return true
		}
	}
	return false
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
	fmt.Printf("当前用户：%s\n", account.Name)
	fmt.Printf("配置文件：%s\n", bashrc)

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
		unmanaged := shared.RemoveManagedBlock(string(data), bashAliasBegin, bashAliasEnd)
		bashBlock, preservedPrompt, preservedColor = rootBashBlockForContent(unmanaged)
	}
	if err := replaceBashConfig(bashrc, bashBlock); err != nil {
		return err
	}

	if err := system.ChownPath(bashrc, account, false); err != nil {
		return err
	}
	if err := os.Chmod(bashrc, 0644); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("已经修改 ~/.bashrc，新的 Bash 配置如下：")
	fmt.Println(bashBlock)
	if preservedPrompt {
		fmt.Println("保留了已有的 PS1 配置")
	}
	if preservedColor {
		fmt.Println("保留了已有的 ls 颜色配置")
	}
	fmt.Println()
	fmt.Println("重新登录或执行 source ~/.bashrc 后生效")
	fmt.Println("Bash 配置完成")
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
