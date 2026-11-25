package podman

import (
	"errors"
	"fmt"
	"strings"

	podmancleanup "snail_tool/internal/container/podman/cleanup"
	podmancompose "snail_tool/internal/container/podman/compose"
	podmanlist "snail_tool/internal/container/podman/list"
	"snail_tool/internal/ui"
)

// ErrBackToRuntimes asks the container entry to return to the Docker / Podman chooser.
var ErrBackToRuntimes = errors.New("back to container runtimes")

func Run(view *ui.UI, current func() (display string, ok bool), backToChooser bool) error {
	for {
		display, ok := current()
		if !ok {
			if backToChooser {
				return ErrBackToRuntimes
			}
			return ui.ErrReturnToMenu
		}
		ui.ClearScreen()
		ui.MenuTitle("容器管理", "Podman")
		ui.PrintInfoCard("容器运行时状态", statusFields(display)...)
		fmt.Println()
		ui.MenuOptionHint("1", "Podman 管理", "查看状态、日志与生命周期操作")
		ui.MenuOptionHint("2", "Compose", "查看、更新与重建项目")
		fmt.Println()
		ui.MenuOptionHint("3", "Podman 配置", "暂无服务代理、构建代理和日志轮转")
		fmt.Println()
		ui.MenuOptionHint("4", "Podman 清理", "容器、网络、镜像、数据卷与构建缓存")
		ui.MenuOptionHint("5", uninstallLabel(display), "可选择保留或永久删除数据")
		ui.MenuExit("0/q", "返回")

		choice, err := view.Ask("请选择：")
		if err != nil {
			return err
		}
		fmt.Println()
		if ui.IsReturnChoice(choice) {
			if backToChooser {
				return ErrBackToRuntimes
			}
			return ui.ErrReturnToMenu
		}

		switch strings.ToLower(choice) {
		case "1":
			ui.RunAction(view, "容器列表与操作失败，已返回容器管理", func() error {
				return podmanlist.Run(view)
			})
		case "2":
			ui.RunAction(view, "Compose 项目失败，已返回容器管理", func() error {
				return podmancompose.Run(view)
			})
		case "3":
			fmt.Println("Podman 暂无这项配置")
			view.Pause()
		case "4":
			ui.RunAction(view, "容器资源清理失败，已返回容器管理", func() error {
				return podmancleanup.Run(view, display)
			})
		case "5":
			uninstalled, err := Uninstall(view)
			if err != nil {
				if errors.Is(err, ui.ErrReturnToMenu) {
					continue
				}
				return err
			}
			if uninstalled {
				return ui.ErrReturnToMenu
			}
		default:
			ui.InvalidChoice()
			view.Pause()
		}
	}
}

func statusFields(display string) []ui.CardField {
	return []ui.CardField{{
		Label: "当前运行时",
		Value: display,
	}}
}

func uninstallLabel(display string) string {
	if display == "Podman" {
		return "Podman 卸载"
	}
	return "卸载 " + display
}
