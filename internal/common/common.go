package common

import (
	"fmt"
	"strings"

	commonbash "snail_tool/internal/common/bash"
	commoncompletion "snail_tool/internal/common/completion"
	commonproxy "snail_tool/internal/common/proxy"
	commonvim "snail_tool/internal/common/vim"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

func Run(view *ui.UI) error {
	for {
		ui.ClearScreen()
		status := currentStatus()
		ui.MenuTitle("通用配置")
		ui.MenuOptionStatusHint("1", "Vim 配置", ui.ConfiguredBadge(status.vim), "~/.vimrc")
		ui.MenuOptionStatus("2", "Bash 配置", ui.ConfiguredBadge(status.bash))
		ui.MenuOptionStatusHint("3", "Bash 自动补全", installedBadge(status.completion), "bash-completion")
		ui.MenuOptionStatus("4", "HTTP/HTTPS 代理", ui.ConfiguredBadge(status.proxy))
		ui.MenuExit("0/q", "返回")
		fmt.Println()

		choice, err := view.Ask("请选择：")
		if err != nil {
			return err
		}
		fmt.Println()

		if shared.IsReturnChoice(choice) {
			return shared.ErrReturnToMenu
		}
		switch strings.ToLower(choice) {
		case "1":
			shared.RunAction(view, "Vim 配置失败，已返回通用配置", func() error {
				return commonvim.Run(view)
			})
		case "2":
			shared.RunAction(view, "Bash 配置失败，已返回通用配置", func() error {
				return commonbash.Run()
			})
		case "3":
			shared.RunAction(view, "Bash 自动补全安装失败，已返回通用配置", func() error {
				return commoncompletion.Run(view)
			})
		case "4":
			shared.RunAction(view, "代理配置失败，已返回通用配置", func() error {
				return commonproxy.Run(view)
			})
		default:
			ui.InvalidChoice()
			view.Pause()
		}
	}
}

type commonStatus struct {
	vim        bool
	bash       bool
	completion bool
	proxy      bool
}

func currentStatus() commonStatus {
	account, err := system.CurrentTargetUser()
	if err != nil {
		return commonStatus{completion: commoncompletion.IsInstalled()}
	}
	return commonStatus{
		vim:        commonvim.IsVimConfigured(account),
		bash:       commonbash.IsBashConfigured(account),
		completion: commoncompletion.IsInstalled(),
		proxy:      commonproxy.IsProxyConfigured(account),
	}
}

func installedBadge(installed bool) string {
	if installed {
		return ui.Badge("已安装", true)
	}
	return ui.Badge("未安装", false)
}
