package counters

import (
	"fmt"

	"snail_tool/internal/ui"
)

func Choose(view *ui.UI, state string, counted, rules int) (string, error) {
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "常用功能设置", "数据包统计")
	ui.PrintInfoCard("数据包统计",
		ui.CardField{Label: "配置状态", Value: state},
		ui.CardField{Label: "作用范围", Value: "本工具 inet snail 规则表"},
		ui.CardField{Label: "已统计规则", Value: fmt.Sprintf("%d / %d 条", counted, rules)},
		ui.CardField{Label: "统计内容", Value: "匹配规则的数据包数量和字节数"},
		ui.CardField{Label: "切换影响", Value: "保存并加载当前完整配置，现有累计计数可能重置"},
	)
	ui.MenuOption("1", "开启数据包统计")
	ui.MenuOption("2", "关闭数据包统计")
	ui.MenuExit("0/q", "返回常用功能设置")
	return ui.AskChoice(view, "请选择", "", map[string]string{"1": "on", "2": "off"})
}
