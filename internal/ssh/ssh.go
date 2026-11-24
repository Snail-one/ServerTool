package ssh

import (
	"fmt"
	"strings"

	"snail_tool/internal/ssh/keys"
	"snail_tool/internal/ssh/rootlogin"
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
		ui.PrintInfoCard("SSH 常用信息", status.fields...)
		ui.MenuOptionStatus("1", "SSH 公钥", ui.ConfiguredBadge(status.keys))
		ui.MenuOptionStatus("2", "SSH 安全策略", ui.ConfiguredBadge(status.security))
		ui.MenuOption("3", "查看生效配置")
		ui.MenuOption("4", "Root SSH 登录策略")
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
			ui.RunAction(view, "SSH 公钥管理失败，已返回 SSH 管理", func() error {
				return keys.Run(view)
			})
		case "2":
			ui.RunAction(view, "SSH 安全策略配置失败，已返回 SSH 管理", func() error {
				return security.Run(view)
			})
		case "3":
			ui.RunAction(view, "查看 SSH 生效配置失败，已返回 SSH 管理", func() error {
				return sshstatus.Show(security.ReadStatus)
			})
		case "4":
			ui.RunAction(view, "Root SSH 登录策略配置失败，已返回 SSH 管理", func() error {
				return rootlogin.Run(view, security.CurrentRootLogin, security.UpdateRootLoginPolicy)
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
	fields   []ui.CardField
}

func currentStatus() sshStatus {
	status := sshStatus{security: security.IsConfigured()}
	userField := ui.CardField{Label: "配置用户", Value: "未检测到"}
	account, err := system.CurrentTargetUser()
	if err != nil {
		userField.Detail = err.Error()
	} else {
		userField.Value = account.Name
		status.keys = keys.IsConfigured(account)
	}
	status.fields = append([]ui.CardField{userField}, security.SummaryFields()...)
	return status
}
