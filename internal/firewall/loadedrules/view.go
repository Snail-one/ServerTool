package loadedrules

import (
	"fmt"
	"strings"

	"snail_tool/internal/ui"
)

func Show(commandOutput func(string, ...string) (string, error)) error {
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "全部已加载规则")
	out, err := commandOutput("nft", "list", "ruleset")
	if err != nil {
		return fmt.Errorf("读取完整 nftables 规则集失败: %w: %s", err, strings.TrimSpace(out))
	}
	if strings.TrimSpace(out) == "" {
		ui.PrintInfoCard("当前没有已加载的 nftables 规则")
	} else {
		fmt.Println(out)
	}
	return nil
}
