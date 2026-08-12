package ssh

import (
	"fmt"
	"strings"

	"snail_tool/internal/shared"
	"snail_tool/internal/ssh/keys"
	"snail_tool/internal/ssh/security"
	sshstatus "snail_tool/internal/ssh/status"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

func Run(view *ui.UI) error {
	for {
		ui.ClearScreen()
		status := currentStatus()
		ui.MenuTitle("SSH 管理")
		ui.MenuOptionStatus("1", "SSH 公钥", ui.ConfiguredBadge(status.keys))
		ui.MenuOptionStatus("2", "SSH 安全策略", ui.ConfiguredBadge(status.security))
		ui.MenuOption("3", "查看生效配置")
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
			shared.RunAction(view, "SSH 公钥管理失败，已返回 SSH 管理", func() error {
				return keys.Run(view)
			})
		case "2":
			shared.RunAction(view, "SSH 安全策略配置失败，已返回 SSH 管理", func() error {
				return security.Run(view)
			})
		case "3":
			shared.RunAction(view, "查看 SSH 生效配置失败，已返回 SSH 管理", func() error {
				return sshstatus.Show()
			})
		default:
			ui.InvalidChoice()
			view.Pause()
		}
	}
}

type sshStatus struct {
	keys     bool
	security bool
}

func currentStatus() sshStatus {
	account, err := system.CurrentTargetUser()
	if err != nil {
		return sshStatus{}
	}
	return sshStatus{
		keys:     keys.IsConfigured(account),
		security: security.IsConfigured(),
	}
}
