package rules

import (
	"encoding/json"
	"fmt"
	"strings"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/firewall/forward/dockermark"
	"snail_tool/internal/firewall/settings/counters"
)

const (
	AccessBegin  = "# ===== BEGIN SNAIL FIREWALL RULES ====="
	AccessEnd    = "# ===== END SNAIL FIREWALL RULES ====="
	ForwardBegin = "# ===== BEGIN SNAIL FORWARD RULES ====="
	ForwardEnd   = "# ===== END SNAIL FORWARD RULES ====="
	RuleMeta     = "# SNAIL RULE "
)

type Block struct {
	begin, end                string
	allowChain, baseChain     string
	hook                      string
	privateChain, publicChain string
}

var AccessBlock = Block{
	begin: AccessBegin, end: AccessEnd,
	allowChain: "source_allow", baseChain: "input", hook: "input",
	privateChain: "private_ports", publicChain: "public_ports",
}

var ForwardBlock = Block{
	begin: ForwardBegin, end: ForwardEnd,
	allowChain: "forward_source_allow", baseChain: "forward", hook: "forward",
	privateChain: "forward_private_ports", publicChain: "forward_public_ports",
}

func FindRuleChain(content string, block Block) (int, int, error) {
	tokens, err := configuration.Tokens(content)
	if err != nil {
		return 0, 0, err
	}
	sourceOpen, sourceClose, inputOpen, inputClose := -1, -1, -1, -1
	for i := 0; i+3 < len(tokens); i++ {
		if tokens[i].Text != "table" || tokens[i+1].Text != "inet" || tokens[i+2].Text != "snail" || tokens[i+3].Text != "{" {
			continue
		}
		end, err := configuration.ClosingBrace(tokens, i+3)
		if err != nil {
			return 0, 0, err
		}
		for j := i + 4; j < end; j++ {
			if j+2 < end && tokens[j].Text == "chain" && tokens[j+2].Text == "{" {
				close, err := configuration.ClosingBrace(tokens, j+2)
				if err != nil {
					return 0, 0, err
				}
				switch tokens[j+1].Text {
				case block.allowChain:
					if sourceOpen >= 0 {
						return 0, 0, fmt.Errorf("发现多个 %s 链，请先整理配置", block.allowChain)
					}
					sourceOpen, sourceClose = j+2, close
				case block.baseChain:
					inputOpen, inputClose = j+2, close
				}
				j = close
			}
		}
		i = end
	}
	if sourceOpen < 0 || inputOpen < 0 {
		return 0, 0, fmt.Errorf("未找到 inet snail 的 %s / %s 链，请先生成防火墙配置", block.allowChain, block.baseChain)
	}
	inbound := false
	for i := inputOpen + 1; i+3 < inputClose; i++ {
		if tokens[i].Text == "type" && tokens[i+1].Text == "filter" && tokens[i+2].Text == "hook" && tokens[i+3].Text == block.hook {
			inbound = true
			break
		}
	}
	if !inbound {
		return 0, 0, fmt.Errorf("%s 链未挂接 %s 过滤 hook，拒绝修改规则", block.baseChain, block.hook)
	}
	jump := false
	for i := inputOpen + 1; i+1 < inputClose; i++ {
		if tokens[i].Text == "goto" && (tokens[i+1].Text == block.privateChain || tokens[i+1].Text == block.publicChain) {
			break
		}
		if tokens[i].Text == "jump" && tokens[i+1].Text == block.allowChain {
			line := strings.LastIndex(content[:tokens[i].Start], "\n") + 1
			jump = strings.TrimSpace(content[line:tokens[i].Start]) == ""
			if jump {
				break
			}
		}
	}
	if !jump {
		return 0, 0, fmt.Errorf("%s 链未在端口分流前无条件 jump %s，拒绝写入可能不生效的规则", block.baseChain, block.allowChain)
	}
	return tokens[sourceOpen].End, tokens[sourceClose].Start, nil
}

func ManagedRules(content string) ([]Rule, error) {
	return ManagedRulesIn(content, AccessBlock)
}

func ManagedRulesIn(content string, block Block) ([]Rule, error) {
	start, end, err := FindRuleChain(content, block)
	if err != nil {
		return nil, err
	}
	beginCount, endCount := strings.Count(content, block.begin), strings.Count(content, block.end)
	if beginCount == 0 && endCount == 0 {
		return nil, nil
	}
	begin, finish := strings.Index(content, block.begin), strings.Index(content, block.end)
	if beginCount != 1 || endCount != 1 || begin < start || finish < begin || finish+len(block.end) > end {
		return nil, fmt.Errorf("规则托管区块标记损坏或不在 %s 链内，拒绝修改", block.allowChain)
	}
	prefix, err := configuration.Tokens(content[start:begin])
	if err != nil || len(prefix) > 0 {
		return nil, fmt.Errorf("规则托管区块前存在其他指令，可能导致规则无法生效；请将手写规则移到托管区块之后")
	}
	body := content[begin+len(block.begin) : finish]
	var rules []Rule
	ids := make(map[string]bool)
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, RuleMeta) {
			continue
		}
		var rule Rule
		decoder := json.NewDecoder(strings.NewReader(strings.TrimPrefix(line, RuleMeta)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&rule); err != nil {
			return nil, fmt.Errorf("规则元数据损坏: %w", err)
		}
		rule, err = NormalizeAllowRule(rule)
		if err != nil {
			return nil, err
		}
		if ids[rule.ID] {
			return nil, fmt.Errorf("规则 ID 重复: %s", rule.ID)
		}
		if _, err := CheckRuleRelations(rule, rules); err != nil {
			return nil, err
		}
		ids[rule.ID] = true
		rules = append(rules, rule)
	}
	actual, err := counters.WithoutStatements(body)
	if err != nil {
		return nil, err
	}
	expected, _ := counters.WithoutStatements(RenderRuleBody(rules))
	if CompactRuleBody(dockermark.WithoutMark(actual)) != CompactRuleBody(dockermark.WithoutMark(expected)) {
		return nil, fmt.Errorf("规则托管区块已被手动修改，元数据与实际规则不一致；请先修正，避免覆盖手动规则")
	}
	return rules, nil
}

func CompactRuleBody(body string) string {
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

func RenderRuleBody(rules []Rule) string {
	var result strings.Builder
	for _, rule := range rules {
		data, _ := json.Marshal(rule)
		result.WriteString("        " + RuleMeta + string(data) + "\n")
		for _, line := range rule.NftLines() {
			result.WriteString("        " + line + "\n")
		}
	}
	return result.String()
}

func ReplaceManagedRules(content string, rules []Rule) (string, error) {
	return ReplaceManagedRulesIn(content, rules, AccessBlock)
}

func ReplaceManagedRulesIn(content string, rules []Rule, section Block) (string, error) {
	if _, err := ManagedRulesIn(content, section); err != nil {
		return "", err
	}
	ids := make(map[string]bool)
	for i, rule := range rules {
		if normalized, err := NormalizeAllowRule(rule); err != nil || normalized != rule {
			return "", fmt.Errorf("规则尚未规范化，拒绝写入: %s", rule.ID)
		}
		if ids[rule.ID] {
			return "", fmt.Errorf("规则 ID 重复: %s", rule.ID)
		}
		if _, err := CheckRuleRelations(rule, rules[:i]); err != nil {
			return "", err
		}
		ids[rule.ID] = true
	}
	countPackets, err := counters.Enabled(content)
	if err != nil {
		return "", err
	}
	body := RenderRuleBody(rules)
	if section.hook == "forward" && strings.Contains(content, dockermark.MarkExpression) {
		body = dockermark.MarkManagedAccepts(body)
	}
	if !countPackets {
		body, err = counters.WithoutStatements(body)
		if err != nil {
			return "", err
		}
	}
	block := section.begin + "\n" + body + "        " + section.end
	begin, end := strings.Index(content, section.begin), strings.Index(content, section.end)
	if begin >= 0 {
		return content[:begin] + block + content[end+len(section.end):], nil
	}
	start, _, err := FindRuleChain(content, section)
	if err != nil {
		return "", err
	}
	return content[:start] + "\n        " + block + "\n" + content[start:], nil
}

func (session Session) SaveRules(version configuration.Version, updated string) (bool, error) {
	saved, err := session.options.Store.SaveAndApply(version, updated)
	if err != nil {
		if !saved {
			return false, err
		}
		return true, fmt.Errorf("%s已保存，但未成功加载；原配置已备份，可输入 r 校验加载配置重试: %w", session.label, err)
	}
	return true, nil
}
