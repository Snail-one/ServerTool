package toolbox

import (
	"fmt"
	"strings"

	commonups "snail_tool/internal/common/ups"
	"snail_tool/internal/shared"
	"snail_tool/internal/startup"
	toolpackages "snail_tool/internal/toolbox/packages"
	"snail_tool/internal/toolbox/startupinfo"
	"snail_tool/internal/ui"
)

// Run displays standalone server tools that do not belong to user or
// development-environment configuration.
func Run(view *ui.UI, report *startup.Report) error {
	tools := toolpackages.NewInventory()
	for {
		ui.ClearScreen()
		ui.MenuTitle("系统工具")
		installed, total, detectionErr := tools.InstalledCount()
		toolStatus := ui.SoftwareBadge(fmt.Sprintf("已安装 %d/%d", installed, total), installed > 0)
		if detectionErr != nil {
			toolStatus = ui.SoftwareBadge("检测失败", false)
		}
		ui.MenuOptionStatus("1", "常用命令行工具", toolStatus)
		ui.MenuOptionStatus("2", "UPS（NUT）", ui.ConfiguredBadge(commonups.IsUPSConfigured()))
		ui.MenuOption("3", "查看本次启动信息")
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
		switch strings.ToLower(strings.TrimSpace(choice)) {
		case "1":
			shared.RunAction(view, "常用命令行工具管理失败，已返回系统工具菜单", func() error {
				return tools.Run(view)
			})
		case "2":
			shared.RunAction(view, "UPS 配置失败，已返回系统工具菜单", func() error {
				return commonups.Run(view)
			})
		case "3":
			shared.RunAction(view, "查看启动信息失败，已返回系统工具菜单", func() error {
				return startupinfo.Show(report)
			})
		default:
			ui.InvalidChoice()
			view.Pause()
		}
	}
}
