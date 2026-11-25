package settings

import (
	"fmt"

	"snail_tool/internal/firewall/loadedrules"
	"snail_tool/internal/firewall/settings/autostart"
	"snail_tool/internal/firewall/settings/counters"
	"snail_tool/internal/firewall/settings/ipforwarding"
	"snail_tool/internal/firewall/settings/ping"
	"snail_tool/internal/firewall/settings/sshaccess"
	"snail_tool/internal/ui"
)

type snapshot struct {
	Fields               []ui.CardField
	AutostartUnavailable bool
	AutostartEnabled     bool
}

type Service struct {
	CommandExists func(string) bool
	CommandOutput func(string, ...string) (string, error)
	Autostart     autostart.Service
	Ping          ping.Service
	Counters      counters.Service
	Forwarding    ipforwarding.Service
	SSH           sshaccess.Service
}

func (service Service) inspect() snapshot {
	boot := service.Autostart.Inspect()
	installed := service.CommandExists("nft")
	live := loadedrules.Snapshot{}
	if installed {
		live = loadedrules.Inspect(service.CommandOutput)
	}
	autostartLabel := ui.StateText(boot.Label, boot.Enabled || boot.State == "enabled-runtime")
	fields := []ui.CardField{{Label: "开机自启", Value: autostartLabel}}
	fields = append(fields, ping.StateFields(installed, live.InputActive, live.JSON, live.Err)...)
	fields = append(fields, counters.StateField(installed, live.InputActive, live.JSON, live.Err))
	fields = append(fields, service.Forwarding.StatusField())
	fields = append(fields, service.SSH.StateField())
	return snapshot{
		Fields: fields, AutostartUnavailable: boot.Err != nil, AutostartEnabled: boot.Enabled,
	}
}

func (service Service) Run(view *ui.UI) error {
	for {
		ui.ClearScreen()
		ui.MenuTitle("防火墙管理", "常用功能设置")
		snapshot := service.inspect()
		ui.PrintInfoCard("功能状态", snapshot.Fields...)
		fmt.Println()
		states := make(map[string]string, len(snapshot.Fields))
		for _, field := range snapshot.Fields {
			states[field.Label] = field.Value
		}
		status := func(label string) string {
			if value := states[label]; value != "" {
				return "[" + value + "]"
			}
			return ""
		}
		switch {
		case snapshot.AutostartUnavailable:
			ui.MenuOptionStatusHint("1", "开机自启设置", status("开机自启"), "查看状态与原因")
		case snapshot.AutostartEnabled:
			ui.MenuOptionStatusHint("1", "关闭开机自启", status("开机自启"), "当前规则保持生效")
		default:
			ui.MenuOptionStatusHint("1", "开启开机自启", status("开机自启"), "开机时加载规则")
		}
		ping := "[IPv4 " + states["ICMP 回显"] + " · IPv6 " + states["ICMPv6 回显"] + "]"
		ui.MenuOptionStatusHint("2", "ICMP 回显（Ping）", ping, "设置 IPv4 / IPv6 入站 Ping")
		ui.MenuOptionStatusHint("3", "数据包统计", status("数据包统计"), "统计规则命中的包数与字节数")
		ui.MenuOptionStatusHint("4", "内核转发", status("内核转发"), "分别设置 IPv4 / IPv6 转发")
		ui.MenuOptionStatusHint("5", "SSH 访问", status("SSH 访问"), "分别设置公网 / 内网 SSH 访问")
		ui.MenuExit("0/q", "返回防火墙管理")
		choice, err := ui.AskChoice(view, "请选择", "", map[string]string{"1": "1", "2": "2", "3": "3", "4": "4", "5": "5"})
		if err != nil {
			return err
		}
		switch choice {
		case "1":
			ui.RunAction(view, "修改开机自启失败，已返回常用功能设置", func() error {
				return service.Autostart.Toggle(view)
			})
		case "2":
			ui.RunAction(view, "设置 ICMP 回显失败，已返回常用功能设置", func() error {
				return service.Ping.Run(view)
			})
		case "3":
			ui.RunAction(view, "设置数据包统计失败，已返回常用功能设置", func() error {
				return service.Counters.Run(view)
			})
		case "4":
			ui.RunAction(view, "设置内核转发失败，已返回常用功能设置", func() error {
				return service.Forwarding.Run(view)
			})
		case "5":
			ui.RunAction(view, "设置 SSH 访问失败，已返回常用功能设置", func() error {
				return service.SSH.Run(view)
			})
		}
	}
}
