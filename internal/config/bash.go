package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/system"
)

const (
	bashAliasBegin   = "# ===== BEGIN SNAIL BASH ALIASES ====="
	bashAliasEnd     = "# ===== END SNAIL BASH ALIASES ====="
	bashCommandBegin = "# ===== BEGIN SNAIL COMMAND ====="
	bashCommandEnd   = "# ===== END SNAIL COMMAND ====="
	bashAliasBlock   = `alias ll='ls -l'
alias la='ls -A'
alias l='ls -lah'`
)

func ConfigureBash() error {
	account, err := system.CurrentTargetUser()
	if err != nil {
		return err
	}

	bashrc := filepath.Join(account.Home, ".bashrc")
	log.Info("配置 Bash 环境...")
	fmt.Printf("当前用户：%s\n", account.Name)
	fmt.Printf("配置文件：%s\n", bashrc)

	if err := ensureFile(bashrc); err != nil {
		return err
	}
	if err := replaceAliases(bashrc); err != nil {
		return err
	}
	commandBlock, err := buildSnailCommandBlock(account)
	if err != nil {
		return err
	}
	if err := writeSnailCommand(bashrc, commandBlock); err != nil {
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
	fmt.Println("已经注册 snail 命令，重新登录或执行 source ~/.bashrc 后生效")
	fmt.Println("运行方式：snail")
	fmt.Println("Bash 配置完成")
	return nil
}

func replaceAliases(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	content := removeManagedBlock(string(data), bashAliasBegin, bashAliasEnd)
	patterns := []string{
		`(?m)^[[:space:]]*#?[[:space:]]*alias ll=.*\n?`,
		`(?m)^[[:space:]]*#?[[:space:]]*alias la=.*\n?`,
		`(?m)^[[:space:]]*#?[[:space:]]*alias l=.*\n?`,
	}
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		content = re.ReplaceAllString(content, "")
	}

	block := fmt.Sprintf("%s\n%s\n%s\n", bashAliasBegin, bashAliasBlock, bashAliasEnd)
	return os.WriteFile(path, []byte(appendBlock(content, block)), 0644)
}

func buildSnailCommandBlock(account *system.Account) (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	command := shellQuote(executable)
	if account.Name != "root" {
		command = "sudo " + command
	}

	return fmt.Sprintf(`snail() {
  %s "$@"
}`, command), nil
}

func writeSnailCommand(path, commandBlock string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	content := removeManagedBlock(string(data), bashCommandBegin, bashCommandEnd)
	block := fmt.Sprintf("%s\n%s\n%s\n", bashCommandBegin, commandBlock, bashCommandEnd)
	return os.WriteFile(path, []byte(appendBlock(content, block)), 0644)
}

func ensureFile(path string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	return file.Close()
}

func removeManagedBlock(content, begin, end string) string {
	re := regexp.MustCompile(`(?s)\n?` + regexp.QuoteMeta(begin) + `.*?` + regexp.QuoteMeta(end) + `\n?`)
	return re.ReplaceAllString(content, "")
}

func managedBlockContent(content, begin, end string) (string, bool) {
	re := regexp.MustCompile(`(?s)` + regexp.QuoteMeta(begin) + `(.*?)` + regexp.QuoteMeta(end))
	match := re.FindStringSubmatch(content)
	if match == nil {
		return "", false
	}
	return match[1], true
}

func appendBlock(content, block string) string {
	content = strings.TrimRight(content, "\n")
	if content == "" {
		return block
	}
	return content + "\n\n" + block
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
