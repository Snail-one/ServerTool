package container

import (
	"fmt"
	"strings"

	containercleanup "snail_tool/internal/container/cleanup"
	"snail_tool/internal/container/list"
	"snail_tool/internal/container/runtime"
	"snail_tool/internal/container/update"
	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

func Run(view *ui.UI) error {
	if err := runtime.Ensure(view); err != nil {
		return err
	}

	for {
		ui.ClearScreen()
		fmt.Println("请选择容器管理操作：")
		fmt.Println("1) 查看容器")
		fmt.Println("2) 管理 Compose 项目（docker compose ls）")
		fmt.Println("3) 管理 Compose 项目（扫描目录）")
		fmt.Println("4) 更新容器")
		fmt.Println("5) 清理容器")
		fmt.Println("0/q) 返回")
		fmt.Println()

		choice, err := view.Ask("输入选项: ")
		if err != nil {
			return err
		}
		fmt.Println()

		switch strings.ToLower(choice) {
		case "1":
			shared.RunAction(view, "容器查看失败，已返回容器管理", func() error {
				return list.Run(view)
			})
		case "2":
			shared.RunAction(view, "Compose 项目管理失败，已返回容器管理", func() error {
				return list.ManageComposeLS(view)
			})
		case "3":
			shared.RunAction(view, "Compose 项目管理失败，已返回容器管理", func() error {
				return list.ManageComposeScan(view)
			})
		case "4":
			shared.RunAction(view, "容器更新失败，已返回容器管理", func() error {
				return update.Run(view)
			})
		case "5":
			shared.RunAction(view, "容器清理失败，已返回容器管理", func() error {
				return containercleanup.Run(view)
			})
		case "0", "q", "exit":
			return shared.ErrReturnToMenu
		default:
			fmt.Println("无效选项，请重新输入")
			view.Pause()
		}
	}
}
