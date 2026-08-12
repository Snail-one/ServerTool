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
		ui.PrintInfoCard("容器运行时状态",
			ui.CardField{Label: "当前运行时", Value: runtime.DisplaySummary(installedRuntimes)},
		)
		fmt.Println()
		ui.MenuOptionHint("1", "管理容器", "查看状态、日志与生命周期操作")
		ui.MenuOptionHint("2", "管理 Compose 项目", "查看、更新与重建项目")
		if hasDocker {
			ui.MenuOptionHint("3", "配置 Docker 服务", "服务代理与日志轮转")
		} else {
			ui.MenuOptionHint("3", "配置 Docker 服务", "仅 Docker 可用；服务代理与日志轮转")
		}
		ui.MenuOptionHint("4", "清理容器资源", "容器、网络、镜像与构建缓存")
		ui.MenuOptionHint("5", fmt.Sprintf("卸载 %s", uninstallName), "可选择保留或永久删除数据")
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
			ui.InvalidChoice()
			view.Pause()
		}
	}
}

func runComposeMenu(view *ui.UI) error {
	for {
		ui.ClearScreen()
		ui.MenuTitle("容器管理", "Compose 项目")
		ui.MenuOptionHint("1", "管理运行中项目", "docker compose ls")
		ui.MenuOptionHint("2", "扫描 Compose 项目", "扫描 Compose 配置目录")
		ui.MenuOptionHint("3", "更新运行中应用", "docker compose pull && docker compose up -d")
		ui.MenuOptionHint("4", "重建运行中项目", "docker compose down && docker compose up -d")
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
			ui.InvalidChoice()
			view.Pause()
		}
	}
}

func runDockerDaemonMenu(view *ui.UI) error {
	for {
		ui.ClearScreen()
		ui.MenuTitle("容器管理", "Docker 服务配置")
		ui.MenuOptionHint("1", "配置服务代理", "systemd 服务代理")
		ui.MenuOptionHint("2", "配置日志轮转", "/etc/docker/daemon.json")
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
			ui.InvalidChoice()
			view.Pause()
		}
	}
}
