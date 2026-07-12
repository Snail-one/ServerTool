package app

import (
	"fmt"
	"strings"

	"snail_tool/internal/cleanup"
	"snail_tool/internal/common"
	"snail_tool/internal/container"
	"snail_tool/internal/environment"
	"snail_tool/internal/shared"
	"snail_tool/internal/ssh"
	"snail_tool/internal/status"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

type App struct {
	ui *ui.UI
}

func New() *App {
	return &App{ui: ui.New()}
}

func (a *App) Run() error {
	for {
		ui.ClearScreen()
		showMenu(currentStatus())
		fmt.Println()

		choice, err := a.ui.Ask("输入选项: ")
		if err != nil {
			return err
		}
		fmt.Println()

		switch strings.ToLower(choice) {
		case "1":
			shared.RunAction(a.ui, "容器管理失败，已返回菜单", func() error {
				return container.Run(a.ui)
			})
		case "2":
			shared.RunAction(a.ui, "SSH 管理失败，已返回菜单", func() error {
				return ssh.Run(a.ui)
			})
		case "3":
			shared.RunAction(a.ui, "常用配置失败，已返回菜单", func() error {
				return common.Run(a.ui)
			})
		case "4":
			shared.RunAction(a.ui, "环境配置失败，已返回菜单", func() error {
				return environment.Run(a.ui)
			})
		case "5":
			shared.RunAction(a.ui, "清理配置失败，已返回菜单", func() error {
				return cleanup.Run(a.ui)
			})
		case "0", "q", "exit":
			fmt.Println("已退出")
			return nil
		default:
			fmt.Println("无效选项，请重新输入")
			a.ui.Pause()
		}
	}
}

func currentStatus() status.Status {
	account, err := system.CurrentTargetUser()
	if err != nil {
		return status.Status{}
	}
	return status.DetectStatus(account)
}

func showMenu(status status.Status) {
	fmt.Println("请选择操作：")
	fmt.Println("1) 容器管理")
	fmt.Println("2) SSH 管理" + statusText(status.SSH))
	fmt.Println("3) 常用配置")
	fmt.Println("4) 环境配置")
	fmt.Println("5) 清理配置")
	fmt.Println("0/q) 退出")
}

func statusText(configured bool) string {
	if configured {
		return " [已配置]"
	}
	return ""
}

func proxyStatusText(configured bool) string {
	if configured {
		return " [已配置代理]"
	}
	return ""
}
