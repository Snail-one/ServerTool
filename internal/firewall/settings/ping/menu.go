package ping

import (
	"snail_tool/internal/ui"
)

func Choose(view *ui.UI, fields []ui.CardField, enabled, found bool) (string, error) {
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "常用功能设置", "ICMP 回显（Ping）")
	ui.PrintInfoCard("ICMP 回显（Ping）", fields...)
	ui.MenuOptionStatus("1", "允许 ICMP 回显", ui.CurrentBadge(found && enabled))
	ui.MenuOptionStatus("2", "禁止 ICMP 回显", ui.CurrentBadge(found && !enabled))
	ui.MenuOptionStatusHint("3", "移除回显配置", ui.CurrentBadge(!found), "删除专用规则，沿用原有规则")
	ui.MenuExit("0/q", "返回常用功能设置")
	return ui.AskChoice(view, "请选择", "", map[string]string{"1": "on", "2": "off", "3": "remove"})
}
