package configuration

import (
	"fmt"
	"strings"

	commonbash "snail_tool/internal/configuration/bash"
	commoncompletion "snail_tool/internal/configuration/completion"
	commonproxy "snail_tool/internal/configuration/proxy"
	"snail_tool/internal/configuration/quicksetup"
	commontmux "snail_tool/internal/configuration/tmux"
	commonvim "snail_tool/internal/configuration/vim"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

func Run(view *ui.UI) error {
	for {
		ui.ClearScreen()
		account, err := system.CurrentTargetUser()
		status := currentStatus(account)
		ui.MenuTitle("配置管理")
		if err != nil {
			ui.PrintInfoCard("配置目标", ui.CardField{Label: "用户", Value: "检测失败", Detail: err.Error()})
		} else {
			ui.PrintInfoCard("配置目标",
				ui.CardField{Label: "用户", Value: account.Name},
				ui.CardField{Label: "主目录", Value: account.Home},
			)
		}
		fmt.Println()
		ui.MenuOptionHint("1", "一键初始化", "SSH 公钥、安全策略、Vim、Bash")
		ui.MenuOptionStatusHint("2", "Vim 配置", ui.ConfiguredBadge(status.vim), "~/.vimrc")
		ui.MenuOptionStatus("3", "Bash 配置", ui.ConfiguredBadge(status.bash))
		ui.MenuOptionStatusHint("4", "Bash 自动补全", ui.InstallationBadge(status.completion), "bash-completion")
		ui.MenuOptionStatusHint("6", "tmux 配置", ui.ConfiguredBadge(status.tmux), "~/.tmux.conf")
		fmt.Println()
		ui.MenuShortcutStatusHint("p", "HTTP/HTTPS 代理", ui.ConfiguredBadge(status.proxy), "")
		ui.MenuExit("0/q", "返回")

		choice, err := view.Ask("请选择：")
		if err != nil {
			return err
		}
		fmt.Println()

		if ui.IsReturnChoice(choice) {
			return ui.ErrReturnToMenu
		}
		switch strings.ToLower(choice) {
		case "1":
			ui.RunAction(view, "一键初始化失败，已返回配置管理", func() error {
				return quicksetup.Run(view)
			})
		case "2":
			ui.RunAction(view, "Vim 配置失败，已返回配置管理", func() error {
				return commonvim.Run(view)
			})
		case "3":
			ui.RunAction(view, "Bash 配置失败，已返回配置管理", func() error {
				return commonbash.Run()
			})
		case "4":
			ui.RunAction(view, "Bash 自动补全安装失败，已返回配置管理", func() error {
				return commoncompletion.Run(view)
			})
		case "6":
			ui.RunAction(view, "tmux 配置失败，已返回配置管理", commontmux.Run)
		case "p":
			ui.RunAction(view, "代理配置失败，已返回配置管理", func() error {
				return commonproxy.Run(view)
			})
		default:
			ui.InvalidChoice()
			view.Pause()
		}
	}
}

type configStatus struct {
	vim        bool
	bash       bool
	completion bool
	proxy      bool
	tmux       bool
}

func currentStatus(account *system.Account) configStatus {
	if account == nil {
		return configStatus{completion: commoncompletion.IsInstalled()}
	}
	return configStatus{
		vim:        commonvim.IsVimConfigured(account),
		bash:       commonbash.IsBashConfigured(account),
		completion: commoncompletion.IsInstalled(),
		proxy:      commonproxy.IsProxyConfigured(account),
		tmux:       commontmux.IsTmuxConfigured(account),
	}
}
