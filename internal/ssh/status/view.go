package status

import (
	"fmt"

	"snail_tool/internal/ui"
)

type Row struct{ Name, Value, State, Directive string }
type Report struct {
	Source string
	Rows   []Row
}

func Run(view *ui.UI, load func() (Report, error)) error {
	choices := map[string]string{"r": "refresh"}
	for {
		ui.ClearScreen()
		report, loadErr := load()
		draw := func() {
			ui.MenuTitle("SSH 管理", "查看生效配置")
			if loadErr != nil {
				ui.PrintWarningCard("无法读取 SSH 生效配置", ui.CardField{Label: "原因", Value: loadErr.Error()})
			} else {
				ui.PrintField("配置来源", report.Source)
				fmt.Println()
				PrintRows(report.Rows)
			}

			fmt.Println()
			ui.MenuShortcutHint("r", "刷新配置", "")
			ui.MenuExit("0/q", "返回 SSH 管理")
		}
		if _, err := ui.AskMenuChoice(view, "请选择", "", choices, draw); err != nil {
			return err
		}
	}
}

func PrintRows(rows []Row) {
	for _, row := range rows {
		switch row.Value {
		case "已开启", "已允许":
			row.Value = ui.StateText(row.Value, true)
		case "已关闭", "已禁用", "已禁止":
			row.Value = ui.StateText(row.Value, false)
		}
		if row.State == "" {
			fmt.Printf("%s %s：%s\n", ui.PrimaryText("-"), ui.InfoText(row.Name), row.Value)
		} else {
			fmt.Printf("%s %s：%s %s\n", ui.PrimaryText("-"), ui.InfoText(row.Name), row.Value, ui.StatusBadge(row.State))
		}
		if row.Directive != "" {
			fmt.Printf("  %s%s\n", ui.MutedText("配置项："), ui.MutedText(row.Directive))
		}
	}
}
