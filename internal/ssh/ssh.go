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
		rootLoginEnabled := status.rootLogin == "允许登录" || status.rootLogin == "仅允许密钥登录" || status.rootLogin == "仅允许强制命令"
		ui.MenuOptionStatus("4", "Root SSH 登录策略", ui.SoftwareBadge(status.rootLogin, rootLoginEnabled))
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
				return sshstatus.Run(view, security.ReadStatus)
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
	keys      bool
	security  bool
	rootLogin string
	fields    []ui.CardField
}

func currentStatus() sshStatus {
	status := sshStatus{security: security.IsConfigured(), rootLogin: "未检测到"}
	userField := ui.CardField{Label: "配置用户", Value: "未检测到"}
	account, err := system.CurrentTargetUser()
	if err != nil {
		userField.Detail = err.Error()
	} else {
		userField.Value = account.Name
		status.keys = keys.IsConfigured(account)
	}
	status.fields = append([]ui.CardField{userField}, security.SummaryFields()...)
	for _, field := range status.fields {
		if field.Label == "Root 登录" {
			status.rootLogin = field.Value
		}
		if field.Label == "配置读取" && field.Value == "失败" {
			status.rootLogin = "检测失败"
			break
		}
	}
	return status
}
