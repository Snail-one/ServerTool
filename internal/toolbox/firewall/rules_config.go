package firewall

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode"

	"snail_tool/internal/shared"
)

const (
	rulesBegin = "# ===== BEGIN SNAIL FIREWALL RULES ====="
	rulesEnd   = "# ===== END SNAIL FIREWALL RULES ====="
	ruleMeta   = "# SNAIL RULE "
)

type configVersion struct {
	content []byte
	exists  bool
}

func readConfigVersion() (configVersion, error) {
	info, err := os.Lstat(configPath)
	if os.IsNotExist(err) {
		return configVersion{}, nil
	}
	if err != nil {
		return configVersion{}, err
	}
	if !info.Mode().IsRegular() {
		return configVersion{}, fmt.Errorf("配置不是普通文件，拒绝修改: %s", configPath)
	}
	data, err := os.ReadFile(configPath)
	return configVersion{content: data, exists: true}, err
}

func checkConfigVersion(expected configVersion) error {
	latest, err := readConfigVersion()
	if err != nil {
		return err
	}
	if latest.exists != expected.exists || !bytes.Equal(latest.content, expected.content) {
		return fmt.Errorf("配置文件已被其他操作修改，未覆盖，请返回后重新操作")
	}
	return nil
}

// Give each firewall edit a distinct backup, including edits in the same second.
func backupFirewallConfig() (string, error) {
	version, err := readConfigVersion()
	if err != nil {
		return "", err
	}
	if !version.exists {
		return "", fmt.Errorf("备份失败：配置文件不存在")
	}
	return (shared.BackupStore{Root: dataDir, Module: "firewall"}).Snapshot(configPath)
}

func firewallBackupDir() string {
	dir, _ := (shared.BackupStore{Root: dataDir, Module: "firewall"}).Directory(configPath)
	return dir
}

type nftToken struct {
	text       string
	start, end int
}

func configTokens(content string) ([]nftToken, error) {
	var result []nftToken
	for i := 0; i < len(content); {
		if unicode.IsSpace(rune(content[i])) {
			i++
			continue
		}
		if content[i] == '#' {
			for i < len(content) && content[i] != '\n' {
				i++
			}
			continue
		}
		start := i
		switch content[i] {
		case '{', '}', ';':
			i++
		case '"', '\'':
			quote := content[i]
			i++
			for i < len(content) && content[i] != quote {
				if content[i] == '\\' {
					i++
				}
				i++
			}
			if i >= len(content) {
				return nil, fmt.Errorf("配置文件字符串未闭合")
			}
			i++
		default:
			for i < len(content) && !unicode.IsSpace(rune(content[i])) && !strings.ContainsRune("{};#", rune(content[i])) {
				i++
			}
		}
		result = append(result, nftToken{text: content[start:i], start: start, end: i})
	}
	return result, nil
}

func closingBrace(tokens []nftToken, open int) (int, error) {
	depth := 0
	for i := open; i < len(tokens); i++ {
		switch tokens[i].text {
		case "{":
			depth++
		case "}":
			depth--
			if depth == 0 {
				return i, nil
			}
		}
	}
	return 0, fmt.Errorf("配置文件花括号未闭合")
}

func findRuleChain(content string) (int, int, error) {
	tokens, err := configTokens(content)
	if err != nil {
		return 0, 0, err
	}
	sourceOpen, sourceClose, inputOpen, inputClose := -1, -1, -1, -1
	for i := 0; i+3 < len(tokens); i++ {
		if tokens[i].text != "table" || tokens[i+1].text != "inet" || tokens[i+2].text != "snail" || tokens[i+3].text != "{" {
			continue
		}
		end, err := closingBrace(tokens, i+3)
		if err != nil {
			return 0, 0, err
		}
		for j := i + 4; j < end; j++ {
			if j+2 < end && tokens[j].text == "chain" && tokens[j+2].text == "{" {
				close, err := closingBrace(tokens, j+2)
				if err != nil {
					return 0, 0, err
				}
				switch tokens[j+1].text {
				case "source_allow":
					if sourceOpen >= 0 {
						return 0, 0, fmt.Errorf("发现多个 source_allow 链，请先整理配置")
					}
					sourceOpen, sourceClose = j+2, close
				case "input":
					inputOpen, inputClose = j+2, close
				}
				j = close
			}
		}
		i = end
	}
	if sourceOpen < 0 || inputOpen < 0 {
		return 0, 0, fmt.Errorf("未找到 inet snail 的 source_allow / input 链，请先生成防火墙配置")
	}
	inbound := false
	for i := inputOpen + 1; i+3 < inputClose; i++ {
		if tokens[i].text == "type" && tokens[i+1].text == "filter" && tokens[i+2].text == "hook" && tokens[i+3].text == "input" {
			inbound = true
			break
		}
	}
	if !inbound {
		return 0, 0, fmt.Errorf("input 链未挂接入站过滤 hook，拒绝修改规则")
	}
	jump := false
	for i := inputOpen + 1; i+1 < inputClose; i++ {
		if tokens[i].text == "goto" && (tokens[i+1].text == "private_ports" || tokens[i+1].text == "public_ports") {
			break
		}
		if tokens[i].text == "jump" && tokens[i+1].text == "source_allow" {
			line := strings.LastIndex(content[:tokens[i].start], "\n") + 1
			jump = strings.TrimSpace(content[line:tokens[i].start]) == ""
			if jump {
				break
			}
		}
	}
	if !jump {
		return 0, 0, fmt.Errorf("input 链未在端口分流前无条件 jump source_allow，拒绝写入可能不生效的规则")
	}
	return tokens[sourceOpen].end, tokens[sourceClose].start, nil
}

func managedRules(content string) ([]allowRule, error) {
	start, end, err := findRuleChain(content)
	if err != nil {
		return nil, err
	}
	beginCount, endCount := strings.Count(content, rulesBegin), strings.Count(content, rulesEnd)
	if beginCount == 0 && endCount == 0 {
		return nil, nil
	}
	begin, finish := strings.Index(content, rulesBegin), strings.Index(content, rulesEnd)
	if beginCount != 1 || endCount != 1 || begin < start || finish < begin || finish+len(rulesEnd) > end {
		return nil, fmt.Errorf("规则托管区块标记损坏或不在 source_allow 链内，拒绝修改")
	}
	prefix, err := configTokens(content[start:begin])
	if err != nil || len(prefix) > 0 {
		return nil, fmt.Errorf("规则托管区块前存在其他指令，可能导致规则无法生效；请将手写规则移到托管区块之后")
	}
	body := content[begin+len(rulesBegin) : finish]
	var rules []allowRule
	ids := make(map[string]bool)
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, ruleMeta) {
			continue
		}
		var rule allowRule
		decoder := json.NewDecoder(strings.NewReader(strings.TrimPrefix(line, ruleMeta)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&rule); err != nil {
			return nil, fmt.Errorf("规则元数据损坏: %w", err)
		}
		rule, err = normalizeAllowRule(rule)
		if err != nil {
			return nil, err
		}
		if ids[rule.ID] {
			return nil, fmt.Errorf("规则 ID 重复: %s", rule.ID)
		}
		if _, err := checkRuleRelations(rule, rules); err != nil {
			return nil, err
		}
		ids[rule.ID] = true
		rules = append(rules, rule)
	}
	actual, err := withoutCounterStatements(body)
	if err != nil {
		return nil, err
	}
	expected, _ := withoutCounterStatements(renderRuleBody(rules))
	if compactRuleBody(actual) != compactRuleBody(expected) {
		return nil, fmt.Errorf("规则托管区块已被手动修改，元数据与实际规则不一致；请先修正，避免覆盖手动规则")
	}
	return rules, nil
}

func compactRuleBody(body string) string {
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

func renderRuleBody(rules []allowRule) string {
	var result strings.Builder
	for _, rule := range rules {
		data, _ := json.Marshal(rule)
		result.WriteString("        " + ruleMeta + string(data) + "\n")
		for _, line := range rule.nftLines() {
			result.WriteString("        " + line + "\n")
		}
	}
	return result.String()
}

func replaceManagedRules(content string, rules []allowRule) (string, error) {
	if _, err := managedRules(content); err != nil {
		return "", err
	}
	ids := make(map[string]bool)
	for i, rule := range rules {
		if normalized, err := normalizeAllowRule(rule); err != nil || normalized != rule {
			return "", fmt.Errorf("规则尚未规范化，拒绝写入: %s", rule.ID)
		}
		if ids[rule.ID] {
			return "", fmt.Errorf("规则 ID 重复: %s", rule.ID)
		}
		if _, err := checkRuleRelations(rule, rules[:i]); err != nil {
			return "", err
		}
		ids[rule.ID] = true
	}
	counters, err := packetCountersEnabled(content)
	if err != nil {
		return "", err
	}
	body := renderRuleBody(rules)
	if !counters {
		body, err = withoutCounterStatements(body)
		if err != nil {
			return "", err
		}
	}
	block := rulesBegin + "\n" + body + "        " + rulesEnd
	begin, end := strings.Index(content, rulesBegin), strings.Index(content, rulesEnd)
	if begin >= 0 {
		return content[:begin] + block + content[end+len(rulesEnd):], nil
	}
	start, _, err := findRuleChain(content)
	if err != nil {
		return "", err
	}
	return content[:start] + "\n        " + block + "\n" + content[start:], nil
}

func saveManagedRules(version configVersion, rules []allowRule) error {
	updated, err := replaceManagedRules(string(version.content), rules)
	if err != nil {
		return err
	}
	if err := writeConfig(updated, version); err != nil {
		return err
	}
	if err := applyConfig([]byte(updated)); err != nil {
		return fmt.Errorf("规则已写入，但未成功加载；原配置已备份，可修正后输入 r 校验加载配置重试: %w", err)
	}
	return nil
}
