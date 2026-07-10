package container

import (
	"fmt"
	"strings"

	containercleanup "snail_tool/internal/container/cleanup"
	"snail_tool/internal/container/daemonlog"
	"snail_tool/internal/container/daemonproxy"
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
		fmt.Println("1) 容器列表与操作")
		fmt.Println("2) Compose 项目")
		fmt.Println("3) Docker daemon 配置")
		fmt.Println("4) 清理容器资源")
		fmt.Println("0/q) 返回")
		fmt.Println()

		choice, err := view.Ask("输入选项: ")
		if err != nil {
			return err
		}
		fmt.Println()

		switch strings.ToLower(choice) {
		case "1":
			shared.RunAction(view, "容器列表与操作失败，已返回容器管理", func() error {
				return list.Run(view)
			})
		case "2":
			shared.RunAction(view, "Compose 项目失败，已返回容器管理", func() error {
				return runComposeMenu(view)
			})
		case "3":
			shared.RunAction(view, "Docker daemon 配置失败，已返回容器管理", func() error {
				return runDockerDaemonMenu(view)
			})
		case "4":
			shared.RunAction(view, "容器资源清理失败，已返回容器管理", func() error {
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

func runComposeMenu(view *ui.UI) error {
	for {
		ui.ClearScreen()
		fmt.Println("请选择 Compose 项目操作：")
		fmt.Println("1) 管理运行中的 Compose 项目（docker compose ls）")
		fmt.Println("2) 管理指定目录中的 Compose 项目（扫描目录）")
		fmt.Println("3) 更新运行中的 Compose 应用（pull 后 up -d）")
		fmt.Println("4) 重建运行中的 Compose 项目（down 后 up -d）")
		fmt.Println("0/q) 返回")
		fmt.Println()

		choice, err := view.Ask("输入选项: ")
		if err != nil {
			return err
		}
		fmt.Println()

		switch strings.ToLower(choice) {
		case "1":
			shared.RunAction(view, "Compose 项目管理失败，已返回 Compose 项目", func() error {
				return list.ManageComposeLS(view)
			})
		case "2":
			shared.RunAction(view, "Compose 项目管理失败，已返回 Compose 项目", func() error {
				return list.ManageComposeScan(view)
			})
		case "3":
			shared.RunAction(view, "Compose 应用更新失败，已返回 Compose 项目", func() error {
				return update.Run(view)
			})
		case "4":
			shared.RunAction(view, "Compose 项目重建失败，已返回 Compose 项目", func() error {
				return update.RebuildRunningComposeProjects(view)
			})
		case "0", "q", "exit":
			return shared.ErrReturnToMenu
		default:
			fmt.Println("无效选项，请重新输入")
			view.Pause()
		}
	}
}

func runDockerDaemonMenu(view *ui.UI) error {
	for {
		ui.ClearScreen()
		fmt.Println("请选择 Docker daemon 配置操作：")
		fmt.Println("1) 配置 Docker daemon 代理")
		fmt.Println("2) 配置 Docker 日志轮转")
		fmt.Println("0/q) 返回")
		fmt.Println()

		choice, err := view.Ask("输入选项: ")
		if err != nil {
			return err
		}
		fmt.Println()

		switch strings.ToLower(choice) {
		case "1":
			shared.RunAction(view, "Docker daemon 代理配置失败，已返回 Docker daemon 配置", func() error {
				return daemonproxy.Run(view)
			})
		case "2":
			shared.RunAction(view, "Docker 日志轮转配置失败，已返回 Docker daemon 配置", func() error {
				return daemonlog.Run(view)
			})
		case "0", "q", "exit":
			return shared.ErrReturnToMenu
		default:
			fmt.Println("无效选项，请重新输入")
			view.Pause()
		}
	}
}
