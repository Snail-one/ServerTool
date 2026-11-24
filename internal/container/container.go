package container

import (
	"errors"
	"fmt"
	"strings"

	containercleanup "snail_tool/internal/container/cleanup"
	"snail_tool/internal/container/compose"
	"snail_tool/internal/container/dockerconfig"
	"snail_tool/internal/container/dockerconfig/clientproxy"
	"snail_tool/internal/container/dockerconfig/daemonlog"
	"snail_tool/internal/container/dockerconfig/daemonproxy"
	"snail_tool/internal/container/list"
	"snail_tool/internal/container/runtime"
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
			ui.MenuOptionHint("3", "Docker 配置", "仅 Docker 可用；代理、日志轮转")
		}
		ui.MenuOptionHint("4", "清理容器资源", "容器、网络、镜像、数据卷与构建缓存")
		ui.MenuOptionHint("5", fmt.Sprintf("卸载 %s", uninstallName), "可选择保留或永久删除数据")
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
			ui.RunAction(view, "容器列表与操作失败，已返回容器管理", func() error {
				return list.Run(view)
			})
		case "2":
			ui.RunAction(view, "Compose 项目失败，已返回容器管理", func() error {
				return compose.Run(view)
			})
		case "3":
			if !hasDocker {
				fmt.Println("Docker 配置仅 Docker 可用")
				view.Pause()
				continue
			}
			ui.RunAction(view, "Docker 配置失败，已返回容器管理", func() error {
				return dockerconfig.Run(view)
			})
		case "4":
			ui.RunAction(view, "容器资源清理失败，已返回容器管理", func() error {
				return containercleanup.Run(view)
			})
		case "5":
			uninstalled, err := runtime.Uninstall(view)
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
