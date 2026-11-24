package counters

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/firewall/internal/nftjson"
	"snail_tool/internal/ui"
)

type Service struct {
	Store    configuration.Store
	IsRoot   func() bool
	Validate func(string) error
}

const Marker = "# SNAIL PACKET COUNTERS: "

// Reuse the menu's kernel snapshot: a saved preference may not be loaded yet.
// Branch/return rules are not counted unless they contain a counter themselves.
func StateField(installed, inputActive bool, liveJSON string, liveErr error) ui.CardField {
	field := ui.CardField{Label: "数据包统计"}
	switch {
	case !installed:
		field.Value = "无法检测"
		return field
	case liveErr != nil:
		field.Value = "检测失败"
		return field
	case !inputActive:
		field.Value = "未启用本工具过滤"
		return field
	}
	objects, err := nftjson.Decode(liveJSON)
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

func Enabled(content string) (bool, error) {
	enabled, found := true, false
	offset := 0
	for _, line := range strings.Split(content, "\n") {
		start := offset
		offset += len(line) + 1
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, strings.TrimSpace(Marker)) {
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
		switch strings.TrimPrefix(line, Marker) {
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
	begin, end := strings.Index(content, configuration.ManagedBegin), strings.Index(content, configuration.ManagedEnd)
	if strings.Count(content, configuration.ManagedBegin) != 1 || strings.Count(content, configuration.ManagedEnd) != 1 || begin > end {
		return 0, 0, fmt.Errorf("需要完整的本工具防火墙配置才能设置数据包统计")
	}
	return begin + len(configuration.ManagedBegin), end, nil
}

func counterStatementEnd(tokens []configuration.Token, at int) (int, error) {
	end := at
	if at+1 < len(tokens) && tokens[at+1].Text == "name" {
		return 0, fmt.Errorf("发现命名计数器，请手动管理，未修改统计配置")
	}
	for end+1 < len(tokens) && (tokens[end+1].Text == "packets" || tokens[end+1].Text == "bytes") {
		if end+2 >= len(tokens) {
			return 0, fmt.Errorf("计数器初始值不完整")
		}
		if _, err := strconv.ParseUint(tokens[end+2].Text, 10, 64); err != nil {
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

func counterRemoval(content string, tokens []configuration.Token, start, end int) counterEdit {
	finish := tokens[end].End
	for finish < len(content) && (content[finish] == ' ' || content[finish] == '\t') {
		finish++
	}
	return counterEdit{start: tokens[start].Start, end: finish}
}

// Used when comparing managed rule content: counter statements do not change
// which traffic a rule allows, and may be omitted when statistics are off.
func WithoutStatements(content string) (string, error) {
	tokens, err := configuration.Tokens(content)
	if err != nil {
		return "", err
	}
	var edits []counterEdit
	for i := 0; i < len(tokens); i++ {
		if tokens[i].Text != "counter" {
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
func Rewrite(content string, enabled bool) (string, int, int, error) {
	tokens, err := configuration.Tokens(content)
	if err != nil {
		return "", 0, 0, err
	}
	var edits []counterEdit
	tables, rules, counted := 0, 0, 0
	for i := 0; i+3 < len(tokens); i++ {
		if tokens[i].Text != "table" || tokens[i+1].Text != "inet" || tokens[i+2].Text != "snail" || tokens[i+3].Text != "{" {
			continue
		}
		tables++
		close, err := configuration.ClosingBrace(tokens, i+3)
		if err != nil {
			return "", 0, 0, err
		}
		for j := i + 4; j+2 < close; j++ {
			if tokens[j].Text != "chain" || tokens[j+2].Text != "{" {
				continue
			}
			chainEnd, err := configuration.ClosingBrace(tokens, j+2)
			if err != nil {
				return "", 0, 0, err
			}
			depth, hasCounter := 0, false
			for k := j + 3; k < chainEnd; k++ {
				token := tokens[k]
				if depth == 0 && k > j+3 && tokens[k-1].Text != "\\" && strings.Contains(content[tokens[k-1].End:token.Start], "\n") {
					hasCounter = false
				}
				if token.Text == "{" {
					depth++
					continue
				}
				if token.Text == "}" {
					depth--
					continue
				}
				if depth != 0 {
					continue
				}
				if token.Text == "counter" {
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
				if (token.Text == "accept" || token.Text == "drop" || token.Text == "reject") && tokens[k-1].Text != "policy" {
					rules++
					if hasCounter {
						counted++
					} else if enabled {
						edits = append(edits, counterEdit{start: token.Start, end: token.Start, text: "counter "})
					}
					hasCounter = false
				}
				if token.Text == ";" || token.Text == "return" || token.Text == "jump" || token.Text == "goto" {
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

func Replace(content string, enabled bool, validate func(string) error) (string, error) {
	begin, end, err := packetCounterRegion(content)
	if err != nil {
		return "", err
	}
	if _, err := Enabled(content); err != nil {
		return "", err
	}
	inner, _, _, err := Rewrite(content[begin:end], enabled)
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
		if strings.HasPrefix(strings.TrimSpace(line), Marker) {
			lines[i], found = Marker+state, true
		}
	}
	updated = strings.Join(lines, "\n")
	if !found {
		updated = strings.Replace(updated, configuration.ManagedBegin, configuration.ManagedBegin+"\n"+Marker+state, 1)
	}
	if err := validate(updated); err != nil {
		return "", err
	}
	return updated, nil
}

func (service Service) Run(view *ui.UI) error {
	if !service.IsRoot() {
		return fmt.Errorf("设置数据包统计需要 root 权限，请使用 sudo 运行本工具")
	}
	version, err := service.Store.Read()
	if err != nil {
		return err
	}
	content := string(version.Content)
	// Preflight managed blocks before presenting an operation we cannot apply.
	if _, err := Replace(content, true, service.Validate); err != nil {
		return err
	}
	begin, end, _ := packetCounterRegion(content)
	_, rules, counted, err := Rewrite(content[begin:end], true)
	if err != nil {
		return err
	}
	state := "已关闭"
	if rules > 0 && counted == rules {
		state = ui.PrimaryText("已开启")
	} else if counted > 0 {
		state = "部分开启"
	}
	for {
		choice, err := Choose(view, state, counted, rules)
		if err != nil {
			return err
		}
		enabled := choice == "on"
		updated, err := Replace(content, enabled, service.Validate)
		if err != nil {
			return err
		}
		action := "关闭"
		if enabled {
			action = "开启"
		}
		ui.ClearScreen()
		ui.MenuTitle("防火墙管理", "常用功能设置", "数据包统计", "确认变更")
		ui.PrintInfoCard("即将应用的数据包统计设置",
			ui.CardField{Label: "当前配置", Value: state},
			ui.CardField{Label: "操作", Value: action + "数据包统计"},
			ui.CardField{Label: "影响", Value: "保存并加载整份当前配置，现有累计计数可能重置"},
		)
		confirmed, err := ui.ConfirmMenuChange(view, "确认"+action+"本工具数据包统计并加载当前配置？", "常用功能设置")
		if errors.Is(err, ui.ErrReturnToMenu) {
			continue
		}
		if err != nil {
			return err
		}
		if !confirmed {
			return ui.ErrReturnToMenu
		}
		saved, err := service.Store.SaveAndApply(version, updated)
		if err != nil {
			if !saved {
				return err
			}
			return fmt.Errorf("数据包统计配置已保存，但未成功加载；原配置已备份，可输入 r 重试: %w", err)
		}
		ui.PrintSuccessCard("数据包统计已"+action, ui.CardField{Label: "生效状态", Value: "配置已保存并加载"})
		return nil
	}
}
