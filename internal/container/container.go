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
		installedRuntimes := runtime.DetectAll()
		hasDocker := false
		for _, installed := range installedRuntimes {
			if installed.Name == "docker" {
				hasDocker = true
			}
		}
		uninstallName := "容器运行时"
		if len(installedRuntimes) == 1 {
			uninstallName = installedRuntimes[0].Display
		} else if len(installedRuntimes) > 1 {
			uninstallName = "Docker 或 Podman"
		}
		ui.ClearScreen()
		ui.MenuTitle("容器管理")
		fmt.Println("当前运行时：" + runtime.DisplaySummary(installedRuntimes))
		fmt.Println()
		fmt.Println("1) 容器列表与操作")
		fmt.Println("2) Compose 项目")
		if hasDocker {
			fmt.Println("3) Docker 服务配置")
		} else {
			fmt.Println("3) Docker 服务配置 [仅 Docker 可用]")
		}
		fmt.Println("4) 清理容器资源")
		fmt.Printf("5) 卸载 %s（可选保留或彻底删除数据）\n", uninstallName)
		fmt.Println("0/q) 返回")
		fmt.Println()

		choice, err := view.Ask("输入选项: ")
		if err != nil {
			return err
		}
		fmt.Println()

		if shared.IsReturnChoice(choice) {
			return shared.ErrReturnToMenu
		}

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
			if !hasDocker {
				fmt.Println("Docker 服务配置仅 Docker 可用")
				view.Pause()
				continue
			}
			shared.RunAction(view, "Docker 服务配置失败，已返回容器管理", func() error {
				return runDockerDaemonMenu(view)
			})
		case "4":
			shared.RunAction(view, "容器资源清理失败，已返回容器管理", func() error {
				return containercleanup.Run(view)
			})
		case "5":
			uninstalled, err := runtime.Uninstall(view)
			if err != nil {
				return err
			}
			if uninstalled {
				return shared.ErrReturnToMenu
			}
			view.Pause()
		default:
			fmt.Println("无效选项，请重新输入")
			view.Pause()
		}
	}
}

func runComposeMenu(view *ui.UI) error {
	for {
		ui.ClearScreen()
		ui.MenuTitle("容器管理", "Compose 项目")
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

		if shared.IsReturnChoice(choice) {
			return shared.ErrReturnToMenu
		}
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
		default:
			fmt.Println("无效选项，请重新输入")
			view.Pause()
		}
	}
}

func runDockerDaemonMenu(view *ui.UI) error {
	for {
		ui.ClearScreen()
		ui.MenuTitle("容器管理", "Docker 服务配置")
		fmt.Println("1) 配置 Docker 服务代理")
		fmt.Println("2) 配置 Docker 日志轮转")
		fmt.Println("0/q) 返回")
		fmt.Println()

		choice, err := view.Ask("输入选项: ")
		if err != nil {
			return err
		}
		fmt.Println()

		if shared.IsReturnChoice(choice) {
			return shared.ErrReturnToMenu
		}
		switch strings.ToLower(choice) {
		case "1":
			shared.RunAction(view, "Docker 服务代理配置失败，已返回 Docker 服务配置", func() error {
				return daemonproxy.Run(view)
			})
		case "2":
			shared.RunAction(view, "Docker 日志轮转配置失败，已返回 Docker 服务配置", func() error {
				return daemonlog.Run(view)
			})
		default:
			fmt.Println("无效选项，请重新输入")
			view.Pause()
		}
	}
}
