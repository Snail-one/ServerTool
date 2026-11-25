package security

import (
	"fmt"

	"snail_tool/internal/ui"
)

func Run(view *ui.UI) error {
	choices := map[string]string{"1": "configure", "r": "refresh"}
	for {
		ui.ClearScreen()
		fields := SummaryFields()
		draw := func() {
			ui.MenuTitle("SSH 管理", "SSH 安全策略")
			ui.PrintInfoCard("当前 SSH 策略", fields...)
			fmt.Println()
			ui.MenuOptionHint("1", "配置安全策略", "设置 SSH 端口和认证策略")
			fmt.Println()
			ui.MenuShortcutHint("r", "刷新状态", "")
			ui.MenuExit("0/q", "返回 SSH 管理")
		}
		choice, err := ui.AskMenuChoice(view, "请选择", "", choices, draw)
		if err != nil {
			return err
		}
		if choice == "configure" {
			ui.ClearScreen()
			ui.MenuTitle("SSH 管理", "SSH 安全策略", "配置安全策略")
			ui.RunAction(view, "配置失败，已返回 SSH 安全策略", func() error {
				return ConfigureSSHSecurity(view)
			})
		}
	}
}
