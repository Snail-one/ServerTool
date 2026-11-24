package peers

import (
	"fmt"
	"strconv"
	"strings"

	"snail_tool/internal/input"
	"snail_tool/internal/ui"
)

type Snapshot struct {
	AllowedIPs []string
	WithPSK    int
	Open       func(int) error
}

func Run(view *ui.UI, name string, load func() (Snapshot, error), quickConfig func() error) error {
	notice := ""
	for {
		snapshot, err := load()
		if err != nil {
			return err
		}
		ui.ClearScreen()
		ui.MenuTitle("WireGuard", name, "管理对端")
		ui.PrintInfoCard("对端概况",
			ui.CardField{Label: "已保存对端", Value: fmt.Sprintf("%d 个", len(snapshot.AllowedIPs))},
			ui.CardField{Label: "已配置预共享密钥", Value: fmt.Sprintf("%d 个", snapshot.WithPSK)},
		)
		fmt.Println()
		if notice != "" {
			ui.PrintField("提示", notice)
			notice = ""
		}
		for i, addresses := range snapshot.AllowedIPs {
			ui.MenuOptionHint(strconv.Itoa(i+1), fmt.Sprintf("对端 %d", i+1), addresses)
		}
		if len(snapshot.AllowedIPs) > 0 {
			fmt.Println()
		}
		ui.MenuShortcutHint("g", "快速配置", "复制本机配置或粘贴对端配置")
		ui.MenuExit("0/q", "返回接口菜单")
		choice, err := view.Ask("请选择对端或操作：")
		if err != nil {
			return err
		}
		if ui.IsReturnChoice(choice) || choice == "" && len(snapshot.AllowedIPs) == 0 {
			return ui.ErrReturnToMenu
		}
		if strings.EqualFold(choice, "g") {
			ui.RunAction(view, "快速配置失败，已返回对端列表", quickConfig)
			continue
		}
		number, err := input.Number(choice, 1, len(snapshot.AllowedIPs))
		if err != nil {
			notice = "请输入对端序号或快捷键"
			continue
		}
		ui.RunAction(view, "对端操作失败，已返回对端列表", func() error {
			return snapshot.Open(number - 1)
		})
	}
}
