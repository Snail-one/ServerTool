package autostart

import "snail_tool/internal/ui"

func Page() {
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "常用功能设置", "开机自启设置")
}

func Confirm(view *ui.UI, status, target string) (bool, error) {
	ui.PrintInfoCard("防火墙开机自启",
		ui.CardField{Label: "当前状态", Value: status},
		ui.CardField{Label: "操作", Value: target + "开机自启"},
		ui.CardField{Label: "影响", Value: "仅修改开机自启，当前已加载规则保持生效"},
	)
	return view.Confirm("是否" + target + "防火墙开机自启？(y/N，0/q 返回)：")
}
