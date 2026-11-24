package sshaccess

import (
	"snail_tool/internal/ui"
)

func Choose(view *ui.UI, ports string, public, private sshScopeStatus) (string, error) {
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "常用功能设置", "SSH 访问")
	ui.PrintInfoCard("SSH 访问",
		ui.CardField{Label: "SSH 端口", Value: ports, Detail: "记录在 set snail_ssh_port，本开关不改服务端口"},
		ui.CardField{Label: "公网", Value: sshAccessLabel(public)},
		ui.CardField{Label: "局域网", Value: sshAccessLabel(private)},
		ui.CardField{Label: "作用范围", Value: "所选范围的 TCP / UDP、IPv4 / IPv6 四份入站名单一起切换"},
		ui.CardField{Label: "不影响", Value: "其他业务端口、转发规则、来源放行，以及 SSH 服务本身"},
	)
	ui.MenuOptionStatus("1", "开放公网 SSH", ui.CurrentBadge(public == ScopeOpen))
	ui.MenuOptionStatus("2", "关闭公网 SSH", ui.CurrentBadge(public == ScopeClosed))
	ui.MenuOptionStatus("3", "开放局域网 SSH", ui.CurrentBadge(private == ScopeOpen))
	ui.MenuOptionStatus("4", "关闭局域网 SSH", ui.CurrentBadge(private == ScopeClosed))
	ui.MenuExit("0/q", "返回常用功能设置")
	return ui.AskChoice(view, "请选择", "", map[string]string{
		"1": "public-on", "2": "public-off", "3": "private-on", "4": "private-off",
	})
}
