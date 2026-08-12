package common

import (
	"fmt"
	"strings"

	commonbash "snail_tool/internal/common/bash"
	commonproxy "snail_tool/internal/common/proxy"
	commonups "snail_tool/internal/common/ups"
	commonvim "snail_tool/internal/common/vim"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

func Run(view *ui.UI) error {
	for {
		ui.ClearScreen()
		status := currentStatus()
		ui.MenuTitle("系统与用户配置")
		ui.MenuOptionStatusHint("1", "Vim 配置", ui.ConfiguredBadge(status.vim), "~/.vimrc")
		ui.MenuOptionStatus("2", "Bash 配置", ui.ConfiguredBadge(status.bash))
		ui.MenuOptionStatus("3", "HTTP/HTTPS 代理", ui.ConfiguredBadge(status.proxy))
		ui.MenuOptionStatus("4", "UPS（NUT）", ui.ConfiguredBadge(status.ups))
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
			shared.RunAction(view, "Vim 配置失败，已返回系统与用户配置", func() error {
				return commonvim.Run(view)
			})
		case "2":
			shared.RunAction(view, "Bash 配置失败，已返回系统与用户配置", func() error {
				return commonbash.Run()
			})
		case "3":
			shared.RunAction(view, "代理配置失败，已返回系统与用户配置", func() error {
				return commonproxy.Run(view)
			})
		case "4":
			shared.RunAction(view, "UPS 配置失败，已返回系统与用户配置", func() error {
				return commonups.Run(view)
			})
		default:
			ui.InvalidChoice()
			view.Pause()
		}
	}
}

type commonStatus struct {
	vim   bool
	bash  bool
	proxy bool
	ups   bool
}

func currentStatus() commonStatus {
	account, err := system.CurrentTargetUser()
	if err != nil {
		return commonStatus{}
	}
	return commonStatus{
		vim:   commonvim.IsVimConfigured(account),
		bash:  commonbash.IsBashConfigured(account),
		proxy: commonproxy.IsProxyConfigured(account),
		ups:   commonups.IsUPSConfigured(),
	}
}
