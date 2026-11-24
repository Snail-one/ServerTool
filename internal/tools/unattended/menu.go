package unattended

import (
	"fmt"

	"snail_tool/internal/ui"
)

func (service manager) menu(view *ui.UI) error {
	choices := map[string]string{"1": "enable", "2": "disable", "3": "simulate"}
	for {
		ui.ClearScreen()
		fields := service.fields()
		draw := func() {
			ui.MenuTitle("系统工具", "unattended-upgrades 管理")
			ui.PrintInfoCard("当前状态", fields...)
			fmt.Println()
			ui.MenuOptionHint("1", "开启自动更新", "如有需要，先安装 unattended-upgrades")
			ui.MenuOption("2", "关闭自动更新")
			ui.MenuOptionHint("3", "模拟运行", "查看当前策略会选择哪些更新")
			ui.MenuExit("0/q", "返回系统工具")
		}
		choice, err := ui.AskMenuChoice(view, "请选择", "", choices, draw)
		if err != nil {
			return err
		}
		ui.RunAction(view, "自动更新操作失败，已返回管理菜单", func() error {
			if choice == "simulate" {
				if !service.root() {
					return fmt.Errorf("模拟自动更新需要 root 权限")
				}
				if !service.exists("unattended-upgrade") {
					return fmt.Errorf("未安装 unattended-upgrades，请先安装")
				}
				return service.run("unattended-upgrade", "--dry-run", "--verbose")
			}
			enable := choice == "enable"
			if err := service.change(view, enable); err != nil {
				return err
			}
			label := "已关闭自动更新"
			if enable {
				label = "已开启自动更新"
			}
			ui.PrintSuccessCard(label)
			return nil
		})
	}
}
