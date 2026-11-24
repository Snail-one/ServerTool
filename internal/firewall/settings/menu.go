package settings

import (
	"fmt"

	"snail_tool/internal/ui"
)

type Snapshot struct {
	Fields               []ui.CardField
	AutostartUnavailable bool
	AutostartEnabled     bool
}

type Actions struct {
	Inspect                                    func() Snapshot
	Autostart, Ping, Counters, Forwarding, SSH func(*ui.UI) error
}

func Run(view *ui.UI, actions Actions) error {
	for {
		ui.ClearScreen()
		ui.MenuTitle("防火墙管理", "常用功能设置")
		snapshot := actions.Inspect()
		ui.PrintInfoCard("功能状态", snapshot.Fields...)
		fmt.Println()
		switch {
		case snapshot.AutostartUnavailable:
			ui.MenuOptionHint("1", "开机自启设置", "查看状态与原因")
		case snapshot.AutostartEnabled:
			ui.MenuOptionHint("1", "关闭开机自启", "当前规则保持生效")
		default:
			ui.MenuOptionHint("1", "开启开机自启", "开机时加载规则")
		}
		ui.MenuOptionHint("2", "ICMP 回显（Ping）", "设置 IPv4 / IPv6 入站 Ping")
		ui.MenuOptionHint("3", "数据包统计", "统计规则命中的包数与字节数")
		ui.MenuOptionHint("4", "内核转发", "分别设置 IPv4 / IPv6 转发")
		ui.MenuOptionHint("5", "SSH 访问", "分别设置公网 / 内网 SSH 访问")
		ui.MenuExit("0/q", "返回防火墙管理")
		choice, err := ui.AskChoice(view, "请选择", "", map[string]string{"1": "1", "2": "2", "3": "3", "4": "4", "5": "5"})
		if err != nil {
			return err
		}
		switch choice {
		case "1":
			ui.RunAction(view, "修改开机自启失败，已返回常用功能设置", func() error {
				return actions.Autostart(view)
			})
		case "2":
			ui.RunAction(view, "设置 ICMP 回显失败，已返回常用功能设置", func() error {
				return actions.Ping(view)
			})
		case "3":
			ui.RunAction(view, "设置数据包统计失败，已返回常用功能设置", func() error {
				return actions.Counters(view)
			})
		case "4":
			ui.RunAction(view, "设置内核转发失败，已返回常用功能设置", func() error {
				return actions.Forwarding(view)
			})
		case "5":
			ui.RunAction(view, "设置 SSH 访问失败，已返回常用功能设置", func() error {
				return actions.SSH(view)
			})
		}
	}
}
