package firewall

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode"

	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

type trafficTable struct{ family, name string }

var defaultTrafficTable = trafficTable{family: "inet", name: "snail"}

func trafficTableFamily(family string) bool {
	switch family {
	case "ip", "ip6", "inet", "arp", "bridge", "netdev":
		return true
	default:
		return false
	}
}

// nft parses its argument text as a command language. Quote names containing
// punctuation so a kernel table name cannot become another nft command.
func trafficTableName(name string) string {
	for i, character := range name {
		if character != '_' && !(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') && !(i > 0 && character >= '0' && character <= '9') {
			return strconv.Quote(name)
		}
	}
	return name
}

func (table trafficTable) label() string {
	return table.family + " " + trafficTableName(table.name)
}

func trafficQuery(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "nft", args...)
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

var queryTrafficRules = func(ctx context.Context, table trafficTable) (string, error) {
	if !trafficTableFamily(table.family) || table.name == "" || strings.ContainsRune(table.name, 0) {
		return "", fmt.Errorf("无效的 nftables 规则表")
	}
	return trafficQuery(ctx, "-a", "list", "table", table.family, trafficTableName(table.name))
}

var queryTrafficTables = func(ctx context.Context) ([]trafficTable, error) {
	output, err := trafficQuery(ctx, "-j", "list", "tables")
	if err != nil {
		return nil, err
	}
	return parseTrafficTables(output)
}

func parseTrafficTables(output string) ([]trafficTable, error) {
	objects, err := decodeRules(output)
	if err != nil {
		return nil, err
	}
	var tables []trafficTable
	seen := make(map[trafficTable]bool)
	for _, object := range objects {
		raw, ok := object["table"].(map[string]any)
		if !ok {
			continue
		}
		family, _ := raw["family"].(string)
		name, _ := raw["name"].(string)
		if !trafficTableFamily(family) || name == "" || strings.IndexFunc(name, unicode.IsControl) >= 0 {
			return nil, fmt.Errorf("读取到无法识别的 nftables 表信息")
		}
		table := trafficTable{family: family, name: name}
		if !seen[table] {
			seen[table] = true
			tables = append(tables, table)
		}
	}
	return tables, nil
}

func chooseTrafficTable(view *ui.UI, current trafficTable) (trafficTable, error) {
	for {
		tables, err := queryTrafficTables(context.Background())
		if err != nil {
			return trafficTable{}, err
		}
		choices := []trafficTable{defaultTrafficTable}
		defaultExists := false
		for _, table := range tables {
			if table == defaultTrafficTable {
				defaultExists = true
			} else {
				choices = append(choices, table)
			}
		}
		ui.ClearScreen()
		ui.MenuTitle("防火墙管理", "实时查看规则", "选择规则表")
		ui.PrintInfoCard("选择规则表", ui.CardField{Label: "当前表", Value: current.label()})
		for i, table := range choices {
			var hints []string
			if table == defaultTrafficTable {
				hints = append(hints, "本工具 · 默认")
				if !defaultExists {
					hints = append(hints, "未加载")
				}
			}
			if table == current {
				hints = append(hints, "当前")
			}
			ui.MenuOptionHint(strconv.Itoa(i+1), table.label(), strings.Join(hints, " · "))
		}
		ui.MenuOption("r", "刷新表列表")
		ui.MenuExit("0/q", "返回当前规则表")
		choice, err := view.Ask("请选择规则表（回车默认 inet snail）")
		if err != nil {
			return trafficTable{}, err
		}
		if shared.IsReturnChoice(choice) {
			return trafficTable{}, shared.ErrReturnToMenu
		}
		if choice == "" {
			return defaultTrafficTable, nil
		}
		if number, err := strconv.Atoi(choice); err == nil && number >= 1 && number <= len(choices) {
			return choices[number-1], nil
		}
	}
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
	selected := defaultTrafficTable
	for {
		err := monitorTrafficTable(view, interval, selected)
		if err != nil {
			return err
		}
		next, err := chooseTrafficTable(view, selected)
		if err == shared.ErrReturnToMenu {
			continue
		}
		if err != nil {
			return err
		}
		selected = next
	}
}

// A nil error requests the table picker; navigation errors return to the menu.
func monitorTrafficTable(view *ui.UI, interval time.Duration, table trafficTable) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	previous := ""
	refresh := func() {
		output, err := queryTrafficRules(ctx, table)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			previous = ""
			ui.ClearScreen()
			ui.MenuTitle("防火墙管理", "实时查看规则")
			ui.PrintInfoCard("规则读取失败",
				ui.CardField{Label: "当前表", Value: table.label()},
				ui.CardField{Label: "原因", Value: err.Error()},
				ui.CardField{Label: "状态", Value: "稍后自动重试"},
			)
			fmt.Print("输入 t 切换规则表；回车或 0/q 后回车返回防火墙管理 ")
			return
		}
		ui.ClearScreen()
		ui.MenuTitle("防火墙管理", "实时查看规则")
		fmt.Println(ui.MutedText("每秒刷新：nft -a list table "+table.label()+" · "+time.Now().Format("15:04:05")) + ui.PrimaryText(" · 变化高亮"))
		fmt.Println()
		fmt.Print(highlightTrafficChanges(previous, output))
		previous = output
		if !strings.HasSuffix(output, "\n") {
			fmt.Println()
		}
		fmt.Println()
		fmt.Print("输入 t 切换规则表；回车或 0/q 后回车返回防火墙管理 ")
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
		if strings.EqualFold(choice, "t") {
			return nil
		}
	}
}
