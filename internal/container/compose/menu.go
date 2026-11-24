package compose

import (
	"fmt"
	"strings"

	"snail_tool/internal/container/compose/update"
	"snail_tool/internal/ui"
)

func Run(view *ui.UI) error {
	for {
		ui.ClearScreen()
		ui.MenuTitle("容器管理", "Compose")
		ui.MenuOptionHint("1", "管理运行中项目", "docker compose ls")
		ui.MenuOptionHint("2", "扫描 Compose 项目", "扫描 Compose 配置目录")
		ui.MenuOptionHint("3", "更新运行中应用", "docker compose pull && docker compose up -d")
		ui.MenuOptionHint("4", "重建运行中项目", "docker compose down && docker compose up -d")
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
			ui.RunAction(view, "Compose 项目管理失败，已返回 Compose", func() error {
				return manageRunning(view)
			})
		case "2":
			ui.RunAction(view, "Compose 项目管理失败，已返回 Compose", func() error {
				return manageScanned(view)
			})
		case "3":
			ui.RunAction(view, "Compose 应用更新失败，已返回 Compose", func() error {
				return update.Run(view)
			})
		case "4":
			ui.RunAction(view, "Compose 项目重建失败，已返回 Compose", func() error {
				return update.RebuildRunningComposeProjects(view)
			})
		default:
			ui.InvalidChoice()
			view.Pause()
		}
	}
}
