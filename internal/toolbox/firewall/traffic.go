package firewall

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

var queryTrafficRules = func(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "nft", "-a", "list", "table", "inet", "snail")
	cmd.WaitDelay = time.Second
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return "", fmt.Errorf("读取防火墙规则超时或已取消: %w", ctx.Err())
	}
	if err != nil {
		return "", fmt.Errorf("读取防火墙规则失败: %w: %s", err, strings.Join(strings.Fields(string(output)), " "))
	}
	return string(output), nil
}

func highlightTrafficChanges(previous, current string) string {
	if previous == "" || previous == current {
		return current
	}
	before, lines := strings.Split(previous, "\n"), strings.Split(current, "\n")
	for i, line := range lines {
		if i >= len(before) {
			if line != "" {
				lines[i] = ui.PrimaryBoldText(line)
			}
			continue
		}
		if before[i] == line || line == "" {
			continue
		}
		old, next := []rune(before[i]), []rune(line)
		start := 0
		for start < len(old) && start < len(next) && old[start] == next[start] {
			start++
		}
		oldEnd, end := len(old), len(next)
		for oldEnd > start && end > start && old[oldEnd-1] == next[end-1] {
			oldEnd--
			end--
		}
		if start == end {
			// A pure deletion has no remaining characters to highlight.
			lines[i] = ui.PrimaryBoldText(line)
		} else {
			lines[i] = string(next[:start]) + ui.PrimaryBoldText(string(next[start:end])) + string(next[end:])
		}
	}
	return strings.Join(lines, "\n")
}

// Input stays on the calling goroutine. Cancelling waits for the display worker
// to stop, so it cannot redraw or consume input after returning to the menu.
func monitorTraffic(view *ui.UI, interval time.Duration) error {
	if !isRoot() {
		return fmt.Errorf("实时查看规则需要 root 权限，请使用 sudo 运行本工具")
	}
	if !commandExists("nft") {
		return fmt.Errorf("未找到 nft，无法实时查看防火墙规则")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	previous := ""
	refresh := func() {
		output, err := queryTrafficRules(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			previous = ""
			ui.ClearScreen()
			ui.MenuTitle("防火墙管理", "实时查看规则")
			ui.PrintInfoCard("规则读取失败",
				ui.CardField{Label: "原因", Value: err.Error()},
				ui.CardField{Label: "状态", Value: "稍后自动重试"},
			)
			fmt.Print("按回车或输入 0/q 后回车，返回防火墙管理 ")
			return
		}
		ui.ClearScreen()
		ui.MenuTitle("防火墙管理", "实时查看规则")
		fmt.Println(ui.MutedText("每秒刷新：nft -a list table inet snail · "+time.Now().Format("15:04:05")) + ui.PrimaryText(" · 变化高亮"))
		fmt.Println()
		fmt.Print(highlightTrafficChanges(previous, output))
		previous = output
		if !strings.HasSuffix(output, "\n") {
			fmt.Println()
		}
		fmt.Println()
		fmt.Print("按回车或输入 0/q 后回车，返回防火墙管理 ")
	}
	refresh()
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				refresh()
			}
		}
	}()
	defer func() { cancel(); <-done }()
	for {
		choice, err := view.ReadLine()
		if err != nil {
			return err
		}
		if choice == "" || shared.IsReturnChoice(choice) {
			return shared.ErrReturnToMenu
		}
	}
}
