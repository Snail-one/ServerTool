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
	return strings.Contains(content, bashAliasBegin) &&
		strings.Contains(content, bashAliasBlock) &&
		strings.Contains(content, bashAliasEnd)
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

	if err := shared.EnsureFile(bashrc); err != nil {
		return err
	}
	if err := replaceAliases(bashrc); err != nil {
		return err
	}

	if err := system.ChownPath(bashrc, account, false); err != nil {
		return err
	}
	if err := os.Chmod(bashrc, 0644); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("已经修改 ~/.bashrc，新的别名配置如下：")
	fmt.Println(bashAliasBlock)
	fmt.Println()
	fmt.Println("重新登录或执行 source ~/.bashrc 后生效")
	fmt.Println("Bash 配置完成")
	return nil
}

func replaceAliases(path string) error {
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

	block := shared.FormatManagedBlock(bashAliasBegin, bashAliasBlock, bashAliasEnd)
	return os.WriteFile(path, []byte(shared.AppendBlock(content, block)), 0644)
}
