package common

import (
	"fmt"
	"strings"

	commonbash "snail_tool/internal/common/bash"
	commoncompletion "snail_tool/internal/common/completion"
	commonproxy "snail_tool/internal/common/proxy"
	commontmux "snail_tool/internal/common/tmux"
	commonvim "snail_tool/internal/common/vim"
	"snail_tool/internal/quicksetup"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

func Run(view *ui.UI) error {
	for {
		ui.ClearScreen()
		status := currentStatus()
		ui.MenuTitle("配置管理")
		ui.MenuOptionHint("1", "一键初始化", "SSH 公钥、安全策略、Vim、Bash")
		ui.MenuOptionStatusHint("2", "Vim 配置", ui.ConfiguredBadge(status.vim), "~/.vimrc")
		ui.MenuOptionStatus("3", "Bash 配置", ui.ConfiguredBadge(status.bash))
		ui.MenuOptionStatusHint("4", "Bash 自动补全", ui.InstallationBadge(status.completion), "bash-completion")
		ui.MenuOptionStatus("5", "HTTP/HTTPS 代理", ui.ConfiguredBadge(status.proxy))
		ui.MenuOptionStatusHint("6", "tmux 配置", ui.ConfiguredBadge(status.tmux), "~/.tmux.conf")
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
			shared.RunAction(view, "一键初始化失败，已返回配置管理", func() error {
				return quicksetup.Run(view)
			})
		case "2":
			shared.RunAction(view, "Vim 配置失败，已返回配置管理", func() error {
				return commonvim.Run(view)
			})
		case "3":
			shared.RunAction(view, "Bash 配置失败，已返回配置管理", func() error {
				return commonbash.Run()
			})
		case "4":
			shared.RunAction(view, "Bash 自动补全安装失败，已返回配置管理", func() error {
				return commoncompletion.Run(view)
			})
		case "5":
			shared.RunAction(view, "代理配置失败，已返回配置管理", func() error {
				return commonproxy.Run(view)
			})
		case "6":
			shared.RunAction(view, "tmux 配置失败，已返回配置管理", commontmux.Run)
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
	tmux       bool
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
		tmux:       commontmux.IsTmuxConfigured(account),
	}
}
