package docker

import (
	"errors"
	"fmt"
	"strings"

	dockercleanup "snail_tool/internal/container/docker/cleanup"
	dockercompose "snail_tool/internal/container/docker/compose"
	"snail_tool/internal/container/docker/dockerconfig"
	"snail_tool/internal/container/docker/dockerconfig/clientproxy"
	"snail_tool/internal/container/docker/dockerconfig/daemonlog"
	"snail_tool/internal/container/docker/dockerconfig/daemonproxy"
	dockerlist "snail_tool/internal/container/docker/list"
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
		logSummary, proxySummary, clientProxySummary := ConfigSummaries()
		ui.ClearScreen()
		ui.MenuTitle("容器管理", "Docker")
		ui.PrintInfoCard("容器运行时状态", statusFields(display, logSummary, proxySummary, clientProxySummary)...)
		fmt.Println()
		ui.MenuOptionHint("1", "Docker 管理", "查看状态、日志与生命周期操作")
		ui.MenuOptionHint("2", "Compose", "查看、更新与重建项目")
		fmt.Println()
		ui.MenuOptionHint("3", "Docker 配置", "服务代理、构建与容器代理、日志轮转")
		fmt.Println()
		ui.MenuOptionHint("4", "Docker 清理", "容器、网络、镜像、数据卷与构建缓存")
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
				return dockerlist.Run(view)
			})
		case "2":
			ui.RunAction(view, "Compose 项目失败，已返回容器管理", func() error {
				return dockercompose.Run(view)
			})
		case "3":
			ui.RunAction(view, "Docker 配置失败，已返回容器管理", func() error {
				return dockerconfig.Run(view)
			})
		case "4":
			ui.RunAction(view, "容器资源清理失败，已返回容器管理", func() error {
				return dockercleanup.Run(view, display)
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

func ConfigSummaries() (string, string, string) {
	return daemonlog.CurrentSummary(), daemonproxy.CurrentSummary(), clientproxy.CurrentSummary()
}

func DetailFields(logSummary, proxySummary, clientProxySummary string) []ui.CardField {
	return []ui.CardField{
		{Label: "容器日志", Value: logSummary},
		{Label: "服务代理", Value: proxySummary},
		{Label: "构建与容器代理", Value: clientProxySummary},
	}
}

func statusFields(display, logSummary, proxySummary, clientProxySummary string) []ui.CardField {
	return append([]ui.CardField{{
		Label: "当前运行时",
		Value: display,
	}}, DetailFields(logSummary, proxySummary, clientProxySummary)...)
}

func uninstallLabel(display string) string {
	if display == "Docker" || strings.HasPrefix(display, "Docker（") {
		return "Docker 卸载" + strings.TrimPrefix(display, "Docker")
	}
	return "卸载 " + display
}
