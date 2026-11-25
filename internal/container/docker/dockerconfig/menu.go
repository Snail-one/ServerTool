package dockerconfig

import (
	"fmt"
	"strings"

	"snail_tool/internal/container/docker/dockerconfig/clientproxy"
	"snail_tool/internal/container/docker/dockerconfig/daemonlog"
	"snail_tool/internal/container/docker/dockerconfig/daemonproxy"
	"snail_tool/internal/ui"
)

func Run(view *ui.UI) error {
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
			ui.RunAction(view, "Docker 服务代理配置失败，已返回 Docker 配置", func() error {
				return daemonproxy.Run(view)
			})
		case "2":
			ui.RunAction(view, "Docker 构建与容器代理配置失败，已返回 Docker 配置", func() error {
				return clientproxy.Run(view)
			})
		case "3":
			ui.RunAction(view, "Docker 日志轮转配置失败，已返回 Docker 配置", func() error {
				return daemonlog.Run(view)
			})
		case "4":
			ui.RunAction(view, "Docker 服务代理移除失败，已返回 Docker 配置", func() error {
				return daemonproxy.Remove(view)
			})
		case "5":
			ui.RunAction(view, "Docker 构建与容器代理移除失败，已返回 Docker 配置", func() error {
				return clientproxy.Remove(view)
			})
		case "6":
			ui.RunAction(view, "Docker 日志轮转移除失败，已返回 Docker 配置", func() error {
				return daemonlog.Remove(view)
			})
		default:
			ui.InvalidChoice()
			view.Pause()
		}
	}
}
