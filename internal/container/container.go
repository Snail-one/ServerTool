package container

import (
	"errors"
	"fmt"
	"strings"

	containercleanup "snail_tool/internal/container/cleanup"
	"snail_tool/internal/container/clientproxy"
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
		logSummary, proxySummary, clientProxySummary := "", "", ""
		if hasDocker {
			logSummary = daemonlog.CurrentSummary()
			proxySummary = daemonproxy.CurrentSummary()
			clientProxySummary = clientproxy.CurrentSummary()
		}
		ui.PrintInfoCard("容器运行时状态", runtimeStatusFields(installedRuntimes, logSummary, proxySummary, clientProxySummary)...)
		fmt.Println()
		ui.MenuOptionHint("1", "管理容器", "查看状态、日志与生命周期操作")
		ui.MenuOptionHint("2", "Compose", "查看、更新与重建项目")
		if hasDocker {
			ui.MenuOptionHint("3", "Docker 配置", "服务代理、构建与容器代理、日志轮转")
		} else {
			ui.MenuOptionHint("3", "Docker 配置", "仅 Docker 可用；代理与日志轮转")
		}
		ui.MenuOptionHint("4", "清理容器资源", "容器、网络、镜像、数据卷与构建缓存")
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
				fmt.Println("Docker 配置仅 Docker 可用")
				view.Pause()
				continue
			}
			shared.RunAction(view, "Docker 配置失败，已返回容器管理", func() error {
				return runDockerConfigMenu(view)
			})
		case "4":
			shared.RunAction(view, "容器资源清理失败，已返回容器管理", func() error {
				return containercleanup.Run(view)
			})
		case "5":
			uninstalled, err := runtime.Uninstall(view)
			if err != nil {
				if errors.Is(err, shared.ErrReturnToMenu) {
					continue
				}
				return err
			}
			if uninstalled {
				return shared.ErrReturnToMenu
			}
		default:
			ui.InvalidChoice()
			view.Pause()
		}
	}
}

func runtimeStatusFields(installedRuntimes []runtime.Runtime, logSummary, proxySummary, clientProxySummary string) []ui.CardField {
	fields := []ui.CardField{{
		Label: "当前运行时",
		Value: runtime.DisplaySummary(installedRuntimes),
	}}
	for _, installed := range installedRuntimes {
		if installed.Name != "docker" {
			continue
		}
		return append(fields,
			ui.CardField{Label: "容器日志", Value: logSummary},
			ui.CardField{Label: "服务代理", Value: proxySummary},
			ui.CardField{Label: "构建与容器代理", Value: clientProxySummary},
		)
	}
	return fields
}

func runComposeMenu(view *ui.UI) error {
	for {
		ui.ClearScreen()
		ui.MenuTitle("容器管理", "Compose")
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
			shared.RunAction(view, "Compose 项目管理失败，已返回 Compose", func() error {
				return list.ManageComposeLS(view)
			})
		case "2":
			shared.RunAction(view, "Compose 项目管理失败，已返回 Compose", func() error {
				return list.ManageComposeScan(view)
			})
		case "3":
			shared.RunAction(view, "Compose 应用更新失败，已返回 Compose", func() error {
				return update.Run(view)
			})
		case "4":
			shared.RunAction(view, "Compose 项目重建失败，已返回 Compose", func() error {
				return update.RebuildRunningComposeProjects(view)
			})
		default:
			ui.InvalidChoice()
			view.Pause()
		}
	}
}

func runDockerConfigMenu(view *ui.UI) error {
	for {
		ui.ClearScreen()
		ui.MenuTitle("容器管理", "Docker 配置")
		ui.PrintSectionCard("当前配置",
			ui.CardField{Label: "服务代理", Value: daemonproxy.CurrentSummary()},
			ui.CardField{Label: "构建与容器代理", Value: clientproxy.CurrentSummary()},
			ui.CardField{Label: "日志轮转", Value: daemonlog.CurrentSummary()},
		)
		fmt.Println()
		ui.MenuOptionHint("1", "配置服务代理", "systemd 服务代理")
		ui.MenuOptionHint("2", "配置构建与容器代理", "用户配置；用于后续构建及新建容器，无需重启 Docker")
		ui.MenuOptionHint("3", "配置日志轮转", "/etc/docker/daemon.json")
		fmt.Println()
		ui.MenuOptionHint("4", "移除服务代理", "移除后重新加载 systemd，并尝试重启 Docker")
		ui.MenuOptionHint("5", "移除构建与容器代理", "用于后续构建及新建容器，无需重启 Docker")
		ui.MenuOptionHint("6", "移除日志轮转", "恢复默认日志设置，并尝试重启 Docker")
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
			shared.RunAction(view, "Docker 服务代理配置失败，已返回 Docker 配置", func() error {
				return daemonproxy.Run(view)
			})
		case "2":
			shared.RunAction(view, "Docker 构建与容器代理配置失败，已返回 Docker 配置", func() error {
				return clientproxy.Run(view)
			})
		case "3":
			shared.RunAction(view, "Docker 日志轮转配置失败，已返回 Docker 配置", func() error {
				return daemonlog.Run(view)
			})
		case "4":
			shared.RunAction(view, "Docker 服务代理移除失败，已返回 Docker 配置", func() error {
				return daemonproxy.Remove(view)
			})
		case "5":
			shared.RunAction(view, "Docker 构建与容器代理移除失败，已返回 Docker 配置", func() error {
				return clientproxy.Remove(view)
			})
		case "6":
			shared.RunAction(view, "Docker 日志轮转移除失败，已返回 Docker 配置", func() error {
				return daemonlog.Remove(view)
			})
		default:
			ui.InvalidChoice()
			view.Pause()
		}
	}
}
