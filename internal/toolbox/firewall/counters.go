package firewall

import (
	"fmt"
	"strconv"
	"strings"

	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

const packetCounterMarker = "# SNAIL PACKET COUNTERS: "

// Reuse the menu's kernel snapshot: a saved preference may not be loaded yet.
// Branch/return rules are not counted unless they contain a counter themselves.
func packetCounterStateField(installed bool, live liveRules) ui.CardField {
	field := ui.CardField{Label: "数据包统计"}
	switch {
	case !installed:
		field.Value = "无法检测"
		return field
	case live.err != nil:
		field.Value = "检测失败"
		return field
	case !live.inputActive:
		field.Value = "未启用本工具过滤"
		return field
	}
	objects, err := decodeRules(live.json)
	if err != nil {
		field.Value = "无法判断"
		return field
	}
	rules, counted := 0, 0
	for _, object := range objects {
		rule, ok := object["rule"].(map[string]any)
		if !ok || rule["family"] != "inet" || rule["table"] != "snail" {
			continue
		}
		expressions, ok := rule["expr"].([]any)
		if !ok || len(expressions) == 0 {
			field.Value = "无法判断"
			return field
		}
		hasCounter, verdict := false, false
		for _, raw := range expressions {
			expression, ok := raw.(map[string]any)
			if !ok || len(expression) != 1 {
				field.Value = "无法判断"
				return field
			}
			if _, ok := expression["counter"]; ok {
				hasCounter = true
			}
			for kind := range expression {
				if kind == "accept" || kind == "drop" || kind == "reject" {
					verdict = true
				}
			}
			if verdict {
				break
			}
		}
		if verdict || hasCounter {
			rules++
			if hasCounter {
				counted++
			}
		}
	}
	switch {
	case counted == 0:
		field.Value = "已关闭"
	case counted == rules:
		field.Value = ui.PrimaryText("已开启")
	default:
		field.Value = "部分开启"
	}
	return field
}

type counterEdit struct {
	start, end int
	text       string
}

func packetCountersEnabled(content string) (bool, error) {
	enabled, found := true, false
	offset := 0
	for _, line := range strings.Split(content, "\n") {
		start := offset
		offset += len(line) + 1
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, strings.TrimSpace(packetCounterMarker)) {
			continue
		}
		begin, end, err := packetCounterRegion(content)
		if err != nil || start < begin || start >= end {
			return false, fmt.Errorf("数据包统计设置不在完整的本工具配置区块内，未修改配置")
		}
		if found {
			return false, fmt.Errorf("数据包统计设置重复，未修改配置")
		}
		found = true
		switch strings.TrimPrefix(line, packetCounterMarker) {
		case "enabled":
			enabled = true
		case "disabled":
			enabled = false
		default:
			return false, fmt.Errorf("数据包统计设置无效，未修改配置")
		}
	}
	return enabled, nil
}

func packetCounterRegion(content string) (int, int, error) {
	begin, end := strings.Index(content, configBegin), strings.Index(content, configEnd)
	if strings.Count(content, configBegin) != 1 || strings.Count(content, configEnd) != 1 || begin > end {
		return 0, 0, fmt.Errorf("需要完整的本工具防火墙配置才能设置数据包统计")
	}
	return begin + len(configBegin), end, nil
}

func counterStatementEnd(tokens []nftToken, at int) (int, error) {
	end := at
	if at+1 < len(tokens) && tokens[at+1].text == "name" {
		return 0, fmt.Errorf("发现命名计数器，请手动管理，未修改统计配置")
	}
	for end+1 < len(tokens) && (tokens[end+1].text == "packets" || tokens[end+1].text == "bytes") {
		if end+2 >= len(tokens) {
			return 0, fmt.Errorf("计数器初始值不完整")
		}
		if _, err := strconv.ParseUint(tokens[end+2].text, 10, 64); err != nil {
			return 0, fmt.Errorf("无法识别计数器初始值，未修改配置")
		}
		end += 2
	}
	return end, nil
}

func applyCounterEdits(content string, edits []counterEdit) string {
	for i := len(edits) - 1; i >= 0; i-- {
		edit := edits[i]
		content = content[:edit.start] + edit.text + content[edit.end:]
	}
	return content
}

func counterRemoval(content string, tokens []nftToken, start, end int) counterEdit {
	finish := tokens[end].end
	for finish < len(content) && (content[finish] == ' ' || content[finish] == '\t') {
		finish++
	}
	return counterEdit{start: tokens[start].start, end: finish}
}

// Used when comparing managed rule content: counter statements do not change
// which traffic a rule allows, and may be omitted when statistics are off.
func withoutCounterStatements(content string) (string, error) {
	tokens, err := configTokens(content)
	if err != nil {
		return "", err
	}
	var edits []counterEdit
	for i := 0; i < len(tokens); i++ {
		if tokens[i].text != "counter" {
			continue
		}
		end, err := counterStatementEnd(tokens, i)
		if err != nil {
			return "", err
		}
		edits = append(edits, counterRemoval(content, tokens, i, end))
		i = end
	}
	return applyCounterEdits(content, edits), nil
}

// Edit only rules in this tool's table. Comments, quoted strings, policies and
// other tables stay intact. Existing matches and terminal verdicts never change.
func rewritePacketCounters(content string, enabled bool) (string, int, int, error) {
	tokens, err := configTokens(content)
	if err != nil {
		return "", 0, 0, err
	}
	var edits []counterEdit
	tables, rules, counted := 0, 0, 0
	for i := 0; i+3 < len(tokens); i++ {
		if tokens[i].text != "table" || tokens[i+1].text != "inet" || tokens[i+2].text != "snail" || tokens[i+3].text != "{" {
			continue
		}
		tables++
		close, err := closingBrace(tokens, i+3)
		if err != nil {
			return "", 0, 0, err
		}
		for j := i + 4; j+2 < close; j++ {
			if tokens[j].text != "chain" || tokens[j+2].text != "{" {
				continue
			}
			chainEnd, err := closingBrace(tokens, j+2)
			if err != nil {
				return "", 0, 0, err
			}
			depth, hasCounter := 0, false
			for k := j + 3; k < chainEnd; k++ {
				token := tokens[k]
				if depth == 0 && k > j+3 && tokens[k-1].text != "\\" && strings.Contains(content[tokens[k-1].end:token.start], "\n") {
					hasCounter = false
				}
				if token.text == "{" {
					depth++
					continue
				}
				if token.text == "}" {
					depth--
					continue
				}
				if depth != 0 {
					continue
				}
				if token.text == "counter" {
					end, err := counterStatementEnd(tokens, k)
					if err != nil {
						return "", 0, 0, err
					}
					hasCounter = true
					if !enabled {
						edits = append(edits, counterRemoval(content, tokens, k, end))
					}
					k = end
					continue
				}
				if (token.text == "accept" || token.text == "drop" || token.text == "reject") && tokens[k-1].text != "policy" {
					rules++
					if hasCounter {
						counted++
					} else if enabled {
						edits = append(edits, counterEdit{start: token.start, end: token.start, text: "counter "})
					}
					hasCounter = false
				}
				if token.text == ";" || token.text == "return" || token.text == "jump" || token.text == "goto" {
					hasCounter = false
				}
			}
			j = chainEnd
		}
		i = close
	}
	if tables != 1 {
		return "", 0, 0, fmt.Errorf("未找到唯一的 inet snail 规则表，请先生成防火墙配置")
	}
	return applyCounterEdits(content, edits), rules, counted, nil
}

func withPacketCounters(content string, enabled bool) (string, error) {
	begin, end, err := packetCounterRegion(content)
	if err != nil {
		return "", err
	}
	if _, err := packetCountersEnabled(content); err != nil {
		return "", err
	}
	inner, _, _, err := rewritePacketCounters(content[begin:end], enabled)
	if err != nil {
		return "", err
	}
	updated := content[:begin] + inner + content[end:]
	state := "disabled"
	if enabled {
		state = "enabled"
	}
	lines := strings.Split(updated, "\n")
	found := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), packetCounterMarker) {
			lines[i], found = packetCounterMarker+state, true
		}
	}
	updated = strings.Join(lines, "\n")
	if !found {
		updated = strings.Replace(updated, configBegin, configBegin+"\n"+packetCounterMarker+state, 1)
	}
	if _, err := managedRules(updated); err != nil {
		return "", err
	}
	if _, _, err := readPingSetting(updated); err != nil {
		return "", err
	}
	return updated, nil
}

func configurePacketCounters(view *ui.UI) error {
	if !isRoot() {
		return fmt.Errorf("设置数据包统计需要 root 权限，请使用 sudo 运行本工具")
	}
	version, err := readConfigVersion()
	if err != nil {
		return err
	}
	content := string(version.content)
	// Preflight managed blocks before presenting an operation we cannot apply.
	if _, err := withPacketCounters(content, true); err != nil {
		return err
	}
	begin, end, _ := packetCounterRegion(content)
	_, rules, counted, err := rewritePacketCounters(content[begin:end], true)
	if err != nil {
		return err
	}
	state := "已关闭"
	if rules > 0 && counted == rules {
		state = ui.PrimaryText("已开启")
	} else if counted > 0 {
		state = "部分开启"
	}
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
	choice, err := askRuleChoice(view, "请选择", "", map[string]string{"1": "on", "2": "off"})
	if err != nil {
		return err
	}
	enabled := choice == "on"
	updated, err := withPacketCounters(content, enabled)
	if err != nil {
		return err
	}
	action := "关闭"
	if enabled {
		action = "开启"
	}
	confirmed, err := view.Confirm("确认" + action + "本工具数据包统计并加载当前配置？(y/N)")
	if err != nil {
		return err
	}
	if !confirmed {
		return shared.ErrReturnToMenu
	}
	if err := writeConfig(updated, version); err != nil {
		return err
	}
	if err := applyConfig([]byte(updated)); err != nil {
		return fmt.Errorf("数据包统计配置已保存，但未成功加载；原配置已备份，可输入 r 重试: %w", err)
	}
	ui.PrintSuccessCard("数据包统计已"+action, ui.CardField{Label: "生效状态", Value: "配置已保存并加载"})
	return nil
}
