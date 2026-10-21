package firewall

import (
	"fmt"
	"sort"
	"strings"
)

type loadedPortSummary struct {
	found      bool
	policy     string
	ports      [2][2][2]portIntervals // public/private, IPv4/IPv6, TCP/UDP.
	private    [2][]string
	exceptions []livePortRule
	system     []string
	otherInput []string
	problem    string
}

func summarizeLoadedPorts(content string) (loadedPortSummary, error) {
	var summary loadedPortSummary
	objects, err := decodeRules(content)
	if err != nil {
		return summary, err
	}
	chains := make(map[string]map[string]any)
	rules := make(map[string][]map[string]any)
	parser := livePortParser{sets: make(map[string]any)}
	elements := make(map[string][]any)
	for _, object := range objects {
		if chain, ok := object["chain"].(map[string]any); ok && chain["hook"] == "input" && !(chain["family"] == "inet" && chain["table"] == "snail" && chain["name"] == "input") {
			summary.otherInput = append(summary.otherInput, fmt.Sprintf("%v %v / %v", chain["family"], chain["table"], chain["name"]))
		}
		for kind, raw := range object {
			body, ok := raw.(map[string]any)
			if !ok || body["family"] != "inet" {
				continue
			}
			if kind == "table" {
				if body["name"] == "snail" {
					summary.found = true
				}
				continue
			}
			if body["table"] != "snail" {
				continue
			}
			name, _ := body["name"].(string)
			switch kind {
			case "chain":
				chains[name] = body
			case "rule":
				chain, _ := body["chain"].(string)
				rules[chain] = append(rules[chain], body)
			case "set":
				value, exists := body["elem"]
				if !exists {
					value = []any{}
				}
				parser.sets[name] = value
			case "element":
				elements[name] = append(elements[name], body["elem"])
			}
		}
	}
	for name, added := range elements {
		if existing, ok := parser.sets[name]; ok {
			parser.sets[name] = append([]any{existing}, added...)
		}
	}
	sort.Strings(summary.otherInput)
	if !summary.found {
		return summary, nil
	}
	input := chains["input"]
	if input == nil || input["hook"] != "input" || input["type"] != "filter" {
		summary.problem = "本工具的 input 入站链缺失或类型、挂载点已改变"
		return summary, nil
	}
	summary.policy, _ = input["policy"].(string)
	if summary.policy != "drop" && summary.policy != "accept" {
		summary.problem = "input 默认策略无法识别"
		return summary, nil
	}
	if err := summary.readInput(parser, chains, rules); err != nil {
		summary.problem = err.Error()
		return summary, nil
	}
	for scope, name := range []string{"public_ports", "private_ports"} {
		chain := chains[name]
		if chain == nil || chain["hook"] != nil {
			summary.problem = name + " 链缺失或被改为基础链"
			return summary, nil
		}
		ports, err := summarizePortChain(parser, rules[name], summary.policy)
		if err != nil {
			summary.problem = name + ": " + err.Error()
			return summary, nil
		}
		summary.ports[scope] = ports
	}
	// A global accept before the source split adds ports to every applicable
	// scope. Source-specific and source-port conditions stay in the exceptions.
	for _, exception := range summary.exceptions {
		if exception.sources != nil || exception.hasSourcePorts {
			continue
		}
		for scope := range summary.ports {
			for family := range summary.ports[scope] {
				for protocol := range summary.ports[scope][family] {
					if exception.families&(1<<family) != 0 && exception.protocols&(1<<protocol) != 0 {
						summary.ports[scope][family][protocol] = unionPorts(append(summary.ports[scope][family][protocol], exception.matchesPorts()...))
					}
				}
			}
		}
	}
	return summary, nil
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func (summary *loadedPortSummary) readInput(parser livePortParser, chains map[string]map[string]any, rules map[string][]map[string]any) error {
	jumped, public := false, false
	for _, raw := range rules["input"] {
		if public {
			break
		} // Rules after the final unconditional goto are unreachable.
		if system := parser.infrastructure(raw); system != "" {
			summary.system = appendUnique(summary.system, system)
			continue
		}
		rule, err := parser.rule(raw)
		if err != nil {
			return fmt.Errorf("input 含无法归纳的规则（句柄 %v）: %w", raw["handle"], err)
		}
		if rule.families == 0 || rule.protocols == 0 {
			continue
		}
		switch rule.verdict {
		case "":
			continue
		case "jump":
			if rule.target != "source_allow" || !rule.unconditional() || jumped || summary.private[0] != nil || summary.private[1] != nil {
				return fmt.Errorf("input 含非标准跳转，无法确认公网 / 内网分流")
			}
			chain := chains["source_allow"]
			if chain == nil || chain["hook"] != nil {
				return fmt.Errorf("source_allow 链缺失或为基础链")
			}
			for _, extra := range rules["source_allow"] {
				parsed, err := parser.rule(extra)
				if err != nil {
					return fmt.Errorf("来源例外含无法归纳的条件（句柄 %v）: %w", extra["handle"], err)
				}
				if parsed.verdict == "return" && parsed.unconditional() {
					break
				}
				if parsed.families == 0 || parsed.protocols == 0 || parsed.verdict == "" {
					continue
				}
				if parsed.verdict != "accept" {
					return fmt.Errorf("来源例外含丢弃、条件返回或其他跳转，无法完整归纳")
				}
				if parsed.canMatchBusiness() {
					summary.exceptions = append(summary.exceptions, parsed)
				}
				if parsed.unconditional() {
					break
				}
			}
			jumped = true
		case "goto":
			if rule.target == "private_ports" && rule.sources != nil && !rule.hasPorts && !rule.hasSourcePorts && rule.protocols == 3 && (rule.families == 1 || rule.families == 2) {
				family := rule.families - 1
				if summary.private[family] != nil {
					return fmt.Errorf("存在多次内网分流，无法完整归纳")
				}
				summary.private[family] = rule.sources
			} else if rule.target == "public_ports" && rule.unconditional() {
				public = true
			} else {
				return fmt.Errorf("input 分流方式已改变，无法完整归纳")
			}
		case "accept":
			if summary.private[0] != nil || summary.private[1] != nil || rule.unconditional() {
				return fmt.Errorf("分流中途或之前存在提前全部放行，无法按公网 / 内网归纳")
			}
			if rule.canMatchBusiness() {
				summary.exceptions = append(summary.exceptions, rule)
			}
		default:
			return fmt.Errorf("input 存在提前丢弃或其他动作，无法完整归纳")
		}
	}
	if !public || summary.private[0] == nil || summary.private[1] == nil {
		return fmt.Errorf("未找到完整的 IPv4 / IPv6 内网及公网分流")
	}
	return nil
}

func summarizePortChain(parser livePortParser, rules []map[string]any, policy string) ([2][2]portIntervals, error) {
	var allowed, remaining [2][2]portIntervals
	for family := range remaining {
		for protocol := range remaining[family] {
			remaining[family][protocol] = allPorts()
		}
	}
	for _, raw := range rules {
		finished := true
		for family := range remaining {
			for protocol := range remaining[family] {
				finished = finished && len(remaining[family][protocol]) == 0
			}
		}
		if finished {
			break
		}
		rule, err := parser.rule(raw)
		if err != nil {
			return allowed, fmt.Errorf("句柄 %v: %w", raw["handle"], err)
		}
		if rule.verdict == "" {
			continue
		}
		if rule.sources != nil || rule.hasSourcePorts {
			return allowed, fmt.Errorf("含来源地址或源端口条件，无法按任意来源汇总")
		}
		if rule.verdict == "return" && rule.unconditional() {
			break
		}
		if rule.verdict != "accept" && rule.verdict != "drop" {
			return allowed, fmt.Errorf("含额外跳转或条件返回")
		}
		for family := range remaining {
			for protocol := range remaining[family] {
				if rule.families&(1<<family) == 0 || rule.protocols&(1<<protocol) == 0 {
					continue
				}
				matched := intersectPorts(remaining[family][protocol], rule.matchesPorts())
				if rule.verdict == "accept" {
					allowed[family][protocol] = unionPorts(append(allowed[family][protocol], matched...))
				}
				remaining[family][protocol] = subtractPorts(remaining[family][protocol], matched)
			}
		}
	}
	if policy == "accept" {
		for family := range allowed {
			for protocol := range allowed[family] {
				allowed[family][protocol] = unionPorts(append(allowed[family][protocol], remaining[family][protocol]...))
			}
		}
	}
	return allowed, nil
}

func (rule livePortRule) description() string {
	family := "IPv4 / IPv6"
	if rule.families == 1 {
		family = "IPv4"
	} else if rule.families == 2 {
		family = "IPv6"
	}
	source := "任意来源"
	if rule.sources != nil {
		source = strings.Join(rule.sources, ", ")
	}
	protocol := "TCP+UDP"
	if rule.protocols == 1 {
		protocol = "TCP"
	} else if rule.protocols == 2 {
		protocol = "UDP"
	} else if !rule.hasProtocol {
		protocol = "全部协议"
	}
	result := fmt.Sprintf("%s · %s → %s / %s", family, source, rule.matchesPorts(), protocol)
	if rule.hasSourcePorts {
		result += " · 仅限源端口 " + rule.sourcePorts.String()
	}
	if rule.id != "" && rule.id != "<nil>" {
		result += " · ID/句柄 " + rule.id
	}
	return result
}

func mergePortExceptions(rules []livePortRule) []livePortRule {
	var result []livePortRule
	indices := make(map[string]int)
	for _, rule := range rules {
		key := fmt.Sprintf("%d/%v/%v/%t/%v/%t/%t/%s", rule.families, rule.sources, rule.ports, rule.hasPorts, rule.sourcePorts, rule.hasSourcePorts, rule.hasProtocol, rule.id)
		if index, exists := indices[key]; exists {
			result[index].protocols |= rule.protocols
		} else {
			indices[key] = len(result)
			result = append(result, rule)
		}
	}
	return result
}
