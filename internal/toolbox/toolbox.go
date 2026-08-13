package toolbox

import (
	"fmt"
	"strings"

	commonups "snail_tool/internal/common/ups"
	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

// Run displays standalone server tools that do not belong to user or
// development-environment configuration.
func Run(view *ui.UI) error {
	for {
		ui.ClearScreen()
		ui.MenuTitle("工具")
		ui.MenuOptionStatus("1", "UPS（NUT）", ui.ConfiguredBadge(commonups.IsUPSConfigured()))
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
			shared.RunAction(view, "UPS 配置失败，已返回工具菜单", func() error {
				return commonups.Run(view)
			})
		default:
			ui.InvalidChoice()
			view.Pause()
		}
	}
}
