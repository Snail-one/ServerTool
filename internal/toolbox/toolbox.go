package toolbox

import (
	"fmt"
	"strings"

	commonups "snail_tool/internal/common/ups"
	"snail_tool/internal/shared"
	toolpackages "snail_tool/internal/toolbox/packages"
	"snail_tool/internal/ui"
)

// Run displays standalone server tools that do not belong to user or
// development-environment configuration.
func Run(view *ui.UI) error {
	for {
		ui.ClearScreen()
		ui.MenuTitle("系统工具")
		ui.MenuOptionStatus("1", "UPS（NUT）", ui.ConfiguredBadge(commonups.IsUPSConfigured()))
		installed, total := toolpackages.InstalledCount()
		ui.MenuOptionStatus("2", "常用命令行工具", ui.SoftwareBadge(fmt.Sprintf("已安装 %d/%d", installed, total), installed > 0))
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
			shared.RunAction(view, "UPS 配置失败，已返回系统工具菜单", func() error {
				return commonups.Run(view)
			})
		case "2":
			shared.RunAction(view, "常用命令行工具管理失败，已返回系统工具菜单", func() error {
				return toolpackages.Run(view)
			})
		default:
			ui.InvalidChoice()
			view.Pause()
		}
	}
}
