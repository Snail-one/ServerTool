package firewall

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

var queryTrafficRules = func(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "nft", "-j", "-a", "list", "ruleset")
	cmd.WaitDelay = time.Second
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return "", fmt.Errorf("读取拦截统计超时或已取消: %w", ctx.Err())
	}
	if err != nil {
		return "", fmt.Errorf("读取拦截统计失败: %w: %s", err, strings.Join(strings.Fields(string(output)), " "))
	}
	return string(output), nil
}

type trafficCounter struct{ packets, bytes uint64 }

type trafficRule struct {
	key, label, action string
	counter            *trafficCounter
	packetsPerSecond   float64
	bytesPerSecond     float64
	rateReady          bool
}

type trafficSnapshot struct {
	at          time.Time
	live        liveRules
	rules       []trafficRule
	total       trafficCounter
	counted     int
	packetsRate float64
	bytesRate   float64
	rateReady   bool
	reset       bool
}

func trafficCount(value any) (uint64, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	result, err := strconv.ParseUint(string(number), 10, 64)
	return result, err == nil
}

// Only count a single anonymous counter after all conditions and before the
// terminal verdict. A counter before a later match/limit can overcount drops.
func trafficRuleCounter(expressions []any) (*trafficCounter, string) {
	var counter *trafficCounter
	counters, counterAt, verdictAt := 0, -1, -1
	action := ""
	for i, expression := range expressions {
		object, _ := expression.(map[string]any)
		if value, ok := object["counter"]; ok {
			counters++
			counterAt = i
			data, _ := value.(map[string]any)
			packets, packetsOK := trafficCount(data["packets"])
			bytes, bytesOK := trafficCount(data["bytes"])
			if packetsOK && bytesOK {
				counter = &trafficCounter{packets: packets, bytes: bytes}
			}
		}
		for _, verdict := range []string{"drop", "reject"} {
			if _, ok := object[verdict]; ok {
				action, verdictAt = verdict, i
			}
		}
	}
	if action == "" {
		return nil, ""
	}
	if counters != 1 || counterAt >= verdictAt {
		return nil, action
	}
	for _, expression := range expressions[counterAt+1 : verdictAt] {
		object, _ := expression.(map[string]any)
		if _, ok := object["log"]; !ok || len(object) != 1 {
			return nil, action
		}
	}
	return counter, action
}

func trafficRuleLabel(chain string, handle uint64, expressions []any) string {
	label := chain
	switch chain {
	case "public_ports":
		label = "公网端口"
	case "private_ports":
		label = "内网端口"
	default:
		for _, expression := range expressions {
			object, _ := expression.(map[string]any)
			match, _ := object["match"].(map[string]any)
			left, _ := match["left"].(map[string]any)
			payload, _ := left["payload"].(map[string]any)
			if payload["field"] == "type" && (match["right"] == "echo-request" || match["right"] == json.Number("8") || match["right"] == json.Number("128")) {
				switch payload["protocol"] {
				case "icmp":
					label = "ICMP 回显"
				case "icmpv6":
					label = "ICMPv6 回显"
				}
			}
		}
	}
	return fmt.Sprintf("%s #%d", label, handle)
}

func parseTrafficSnapshot(output string, at time.Time) (trafficSnapshot, error) {
	snapshot := trafficSnapshot{at: at, live: parseLiveRules(output)}
	if snapshot.live.err != nil {
		return snapshot, snapshot.live.err
	}
	objects, err := decodeRules(output)
	if err != nil {
		return snapshot, err
	}
	for _, object := range objects {
		raw, ok := object["rule"].(map[string]any)
		if !ok || raw["family"] != "inet" || raw["table"] != "snail" {
			continue
		}
		expressions, _ := raw["expr"].([]any)
		counter, action := trafficRuleCounter(expressions)
		if action == "" {
			continue
		}
		handle, ok := trafficCount(raw["handle"])
		if !ok || handle == 0 {
			return snapshot, fmt.Errorf("拦截规则缺少有效句柄，无法稳定跟踪统计")
		}
		chain, _ := raw["chain"].(string)
		identity, _ := json.Marshal(normalizeRuleValue(expressions, "expr"))
		rule := trafficRule{
			key:   fmt.Sprintf("%s/%d/%x", chain, handle, sha256.Sum256(identity)),
			label: trafficRuleLabel(chain, handle, expressions), action: action, counter: counter,
		}
		if counter != nil {
			snapshot.total.packets += counter.packets
			snapshot.total.bytes += counter.bytes
			snapshot.counted++
		}
		snapshot.rules = append(snapshot.rules, rule)
	}
	return snapshot, nil
}

func (snapshot *trafficSnapshot) compare(previous *trafficSnapshot) {
	if previous == nil || !snapshot.at.After(previous.at) {
		return
	}
	elapsed := snapshot.at.Sub(previous.at).Seconds()
	old := make(map[string]*trafficCounter)
	for _, rule := range previous.rules {
		if rule.counter != nil {
			old[rule.key] = rule.counter
		}
	}
	snapshot.rateReady = snapshot.counted > 0
	for i := range snapshot.rules {
		rule := &snapshot.rules[i]
		if rule.counter == nil {
			continue
		}
		before := old[rule.key]
		delete(old, rule.key)
		if before == nil || rule.counter.packets < before.packets || rule.counter.bytes < before.bytes {
			snapshot.reset, snapshot.rateReady = true, false
			continue
		}
		rule.packetsPerSecond = float64(rule.counter.packets-before.packets) / elapsed
		rule.bytesPerSecond = float64(rule.counter.bytes-before.bytes) / elapsed
		rule.rateReady = true
		snapshot.packetsRate += rule.packetsPerSecond
		snapshot.bytesRate += rule.bytesPerSecond
	}
	if len(old) > 0 {
		snapshot.reset, snapshot.rateReady = true, false
	}
}

func trafficBytes(value float64) string {
	unit := "B"
	for _, next := range []string{"KiB", "MiB", "GiB", "TiB", "PiB", "EiB"} {
		if value < 1024 {
			break
		}
		value /= 1024
		unit = next
	}
	return fmt.Sprintf("%.1f %s", value, unit)
}

func printTrafficSnapshot(snapshot trafficSnapshot) {
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "实时拦截统计")
	total, rate := "无有效计数器", "采样中"
	if snapshot.counted > 0 {
		total = fmt.Sprintf("%d 包 · %s", snapshot.total.packets, trafficBytes(float64(snapshot.total.bytes)))
	}
	if snapshot.rateReady {
		rate = fmt.Sprintf("%.1f 包/秒 · %s/秒", snapshot.packetsRate, trafficBytes(snapshot.bytesRate))
	} else if snapshot.reset {
		rate = "规则或计数器已变化，重新采样"
	}
	ui.PrintInfoCard("实时拦截统计",
		firewallStateField(true, snapshot.live),
		ui.CardField{Label: "累计拦截", Value: total},
		ui.CardField{Label: "当前速率", Value: rate},
		ui.CardField{Label: "统计覆盖", Value: fmt.Sprintf("%d / %d 条直接拦截规则有有效计数器", snapshot.counted, len(snapshot.rules))},
		ui.CardField{Label: "更新时间", Value: snapshot.at.Format("15:04:05") + " · 每秒刷新"},
	)
	if len(snapshot.rules) == 0 {
		fmt.Println("当前没有可列出的直接 drop / reject 规则")
	} else {
		fmt.Println(ui.InfoText(ui.TableCell("规则 / 句柄", 24) + ui.TableCell("动作", 8) + ui.TableCell("累计包数", 14) + ui.TableCell("累计流量", 14) + ui.TableCell("包/秒", 12) + "流量/秒"))
		for _, rule := range snapshot.rules {
			packets, bytes, packetRate, byteRate := "无有效计数器", "—", "—", "—"
			if rule.counter != nil {
				packets, bytes = strconv.FormatUint(rule.counter.packets, 10), trafficBytes(float64(rule.counter.bytes))
			}
			if rule.rateReady {
				packetRate, byteRate = fmt.Sprintf("%.1f", rule.packetsPerSecond), trafficBytes(rule.bytesPerSecond)
			}
			fmt.Println(ui.TableCell(rule.label, 24) + ui.TableCell(rule.action, 8) + ui.TableCell(packets, 14) + ui.TableCell(bytes, 14) + ui.TableCell(packetRate, 12) + byteRate)
		}
	}
	fmt.Println()
	fmt.Println(ui.MutedText("仅统计 inet snail 的有效拦截计数器；默认策略丢包和其他表不计入。"))
	fmt.Println(ui.MutedText("累计包数不是连接数或攻击次数；重新加载规则可能归零。"))
	fmt.Print("按回车或输入 0/q 后回车，返回防火墙管理 ")
}

// Input stays on the calling goroutine. Cancelling waits for the display worker
// to stop, so it cannot redraw or consume input after returning to the menu.
func monitorTraffic(view *ui.UI, interval time.Duration) error {
	if !isRoot() {
		return fmt.Errorf("查看实时拦截统计需要 root 权限，请使用 sudo 运行本工具")
	}
	if !commandExists("nft") {
		return fmt.Errorf("未找到 nft，无法读取实时拦截统计")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var previous *trafficSnapshot
	refresh := func() {
		output, err := queryTrafficRules(ctx)
		if ctx.Err() != nil {
			return
		}
		var snapshot trafficSnapshot
		if err == nil {
			snapshot, err = parseTrafficSnapshot(output, time.Now())
		}
		if err != nil {
			previous = nil
			ui.ClearScreen()
			ui.MenuTitle("防火墙管理", "实时拦截统计")
			ui.PrintInfoCard("统计读取失败",
				ui.CardField{Label: "原因", Value: err.Error()},
				ui.CardField{Label: "状态", Value: "暂停统计，稍后自动重试"},
			)
			fmt.Print("按回车或输入 0/q 后回车，返回防火墙管理 ")
			return
		}
		snapshot.compare(previous)
		printTrafficSnapshot(snapshot)
		previous = &snapshot
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
