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

func Show(load func() (Report, error)) error {
	report, err := load()
	if err != nil {
		return err
	}

	fmt.Println(ui.PrimaryBoldText("当前 SSH 安全配置："))
	fmt.Println()
	fmt.Println(ui.InfoText("配置来源："), report.Source)
	fmt.Println()
	PrintRows(report.Rows)
	return nil
}

func PrintRows(rows []Row) {
	for _, row := range rows {
		if row.Value == "已开启" {
			row.Value = ui.PrimaryText(row.Value)
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
