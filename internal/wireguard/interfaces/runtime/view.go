package runtime

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"snail_tool/internal/input"
	"snail_tool/internal/ui"
)

func Run(view *ui.UI, name string, interval time.Duration, read func(context.Context, string) ([]ui.CardField, map[string][]ui.CardField, error), shortKey func(string) string) error {
	ctx, cancel := context.WithCancel(context.Background())
	refresh := func() {
		if ctx.Err() != nil {
			return
		}
		queryCtx, endQuery := context.WithTimeout(ctx, 3*time.Second)
		defer endQuery()
		fields, peers, err := read(queryCtx, name)
		if ctx.Err() != nil {
			return
		}
		ui.ClearScreen()
		ui.MenuTitle("WireGuard", name, "查看运行状态")
		fmt.Println(ui.MutedText("每秒刷新 · " + time.Now().Format("15:04:05")))
		fmt.Println()
		if err == nil {
			printStatus(fields, peers, shortKey)
		} else {
			ui.PrintInfoCard("读取运行状态失败", ui.CardField{Label: "原因", Value: err.Error()})
		}
		ui.MenuExit("0/q", "返回接口菜单")
		fmt.Print(ui.PrimaryText("❯ ") + "回车返回： ")
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
		if errors.Is(err, input.ErrInvalid) {
			continue
		}
		if err != nil {
			return err
		}
		if choice == "" || ui.IsReturnChoice(choice) {
			return ui.ErrReturnToMenu
		}
	}
}

func printStatus(fields []ui.CardField, peers map[string][]ui.CardField, shortKey func(string) string) {
	ui.PrintInfoCard("WireGuard 运行状态", fields...)
	keys := make([]string, 0, len(peers))
	for key := range peers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for i, key := range keys {
		peerFields := []ui.CardField{{Label: "公钥", Value: shortKey(key)}}
		ui.PrintSectionCard(fmt.Sprintf("对端 %d", i+1), append(peerFields, peers[key]...)...)
	}
}
