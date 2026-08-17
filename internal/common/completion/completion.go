package completion

import (
	"fmt"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

const (
	packageName                = "bash-completion"
	bashCompletionScript       = "/usr/share/bash-completion/bash_completion"
	legacyBashCompletionScript = "/etc/bash_completion"
)

var (
	commandExists = system.CommandExists
	commandOutput = system.Output
	commandRun    = system.Run
	fileExists    = system.FileExists
)

func Run(view *ui.UI) error {
	return Configure(view)
}

func IsInstalled() bool {
	return completionScriptPresent() || packageInstalled()
}

func Configure(view *ui.UI) error {
	fmt.Println("检查 bash-completion 是否安装...")
	if IsInstalled() {
		log.Info("bash-completion 已安装")
		fmt.Println()
		ui.PrintInfoCard("Bash 自动补全已安装",
			ui.CardField{Label: "软件包", Value: packageName},
			ui.CardField{Label: "脚本", Value: completionScriptPath()},
		)
		return nil
	}

	log.Warn("未检测到 bash-completion")
	confirmed, err := view.Confirm("是否安装 bash-completion？(y/N)：")
	if err != nil {
		return err
	}
	if !confirmed {
		fmt.Println("已取消安装")
		return nil
	}

	if err := installBashCompletion(); err != nil {
		return err
	}
	if !IsInstalled() {
		return fmt.Errorf("安装后仍未检测到 bash-completion")
	}

	fmt.Println()
	ui.PrintSuccessCard("Bash 自动补全安装完成",
		ui.CardField{Label: "软件包", Value: packageName},
		ui.CardField{Label: "脚本", Value: completionScriptPath()},
		ui.CardField{Label: "生效方式", Value: "重新打开终端后生效"},
	)
	return nil
}

func completionScriptPresent() bool {
	return fileExists(bashCompletionScript) || fileExists(legacyBashCompletionScript)
}

func completionScriptPath() string {
	if fileExists(bashCompletionScript) {
		return bashCompletionScript
	}
	if fileExists(legacyBashCompletionScript) {
		return legacyBashCompletionScript
	}
	return bashCompletionScript
}

func packageInstalled() bool {
	switch {
	case commandExists("dpkg-query"):
		out, err := commandOutput("dpkg-query", "-W", "-f=${Status}", packageName)
		return err == nil && strings.Contains(out, "install ok installed")
	case commandExists("rpm"):
		_, err := commandOutput("rpm", "-q", packageName)
		return err == nil
	case commandExists("pacman"):
		_, err := commandOutput("pacman", "-Q", packageName)
		return err == nil
	default:
		return false
	}
}

func installBashCompletion() error {
	log.Info("开始安装 bash-completion...")

	switch {
	case commandExists("apt"):
		if err := commandRun("apt", "update"); err != nil {
			return fmt.Errorf("apt update 失败: %w", err)
		}
		return commandRun("apt", "install", "-y", packageName)
	case commandExists("apt-get"):
		if err := commandRun("apt-get", "update"); err != nil {
			return fmt.Errorf("apt-get update 失败: %w", err)
		}
		return commandRun("apt-get", "install", "-y", packageName)
	case commandExists("dnf"):
		return commandRun("dnf", "install", "-y", packageName)
	case commandExists("yum"):
		return commandRun("yum", "install", "-y", packageName)
	case commandExists("pacman"):
		return commandRun("pacman", "-Sy", "--noconfirm", packageName)
	case commandExists("zypper"):
		return commandRun("zypper", "--non-interactive", "install", packageName)
	default:
		return fmt.Errorf("未识别支持的包管理器，请手动安装 %s", packageName)
	}
}
