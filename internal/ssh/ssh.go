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
		fmt.Println("1) SSH 公钥" + statusText(status.keys))
		fmt.Println("2) SSH 安全策略" + statusText(status.security))
		fmt.Println("3) 查看生效配置")
		fmt.Println("0/q) 返回")
		fmt.Println()

		choice, err := view.Ask("输入选项: ")
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
			fmt.Println("无效选项，请重新输入")
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

func statusText(configured bool) string {
	if configured {
		return " [已配置]"
	}
	return ""
}
