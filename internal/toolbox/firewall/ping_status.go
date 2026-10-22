package firewall

import (
	"fmt"
	"strings"

	"snail_tool/internal/ui"
)

const (
	echoAllow = 1 << iota
	echoBlock
	echoReturn
	echoUnknown
)

// echoMatch evaluates a new external echo request. Interface names and source
// addresses other than loopback remain conditional, rather than guessed.
func echoMatch(match map[string]any, ipv6 bool) int {
	op := match["op"]
	if op != "==" && op != "in" && op != "!=" {
		return 2
	}
	left, _ := match["left"].(map[string]any)
	var candidates []string
	if payload, ok := left["payload"].(map[string]any); ok {
		protocol, field := payload["protocol"], payload["field"]
		if protocol == "tcp" || protocol == "udp" || (protocol == "icmp" && ipv6) || (protocol == "icmpv6" && !ipv6) || (protocol == "ip" && ipv6) || (protocol == "ip6" && !ipv6) {
			return 0
		}
		if field == "type" && protocol == "icmp" {
			candidates = []string{"echo-request", "8"}
		} else if field == "type" && protocol == "icmpv6" {
			candidates = []string{"echo-request", "128"}
		} else {
			return 2
		}
	} else if meta, ok := left["meta"].(map[string]any); ok {
		switch meta["key"] {
		case "nfproto":
			candidates = []string{"ipv4", "2"}
			if ipv6 {
				candidates = []string{"ipv6", "10"}
			}
		case "l4proto":
			candidates = []string{"icmp", "1"}
			if ipv6 {
				candidates = []string{"ipv6-icmp", "icmpv6", "58"}
			}
		case "iifname":
			if match["right"] != "lo" {
				return 2
			}
			if op == "!=" {
				return 1
			}
			return 0
		default:
			return 2
		}
	} else if ct, ok := left["ct"].(map[string]any); ok && ct["key"] == "state" {
		candidates = []string{"new"}
	} else {
		return 2
	}
	values, err := (livePortParser{}).values(match["right"], 0)
	if err != nil {
		return 2
	}
	found := false
	for _, value := range values {
		if _, complex := value.(map[string]any); complex || value == nil || strings.HasPrefix(fmt.Sprint(value), "@") {
			return 2
		}
		for _, candidate := range candidates {
			found = found || fmt.Sprint(value) == candidate
		}
	}
	if op == "!=" {
		found = !found
	}
	if found {
		return 1
	}
	return 0
}

// Inspect expressions, not comments or ownership tags: manual changes and
// earlier terminal verdicts must not be presented as an active tool switch.
func echoRule(raw map[string]any, ipv6 bool) (condition int, verdict, target string) {
	condition = 1
	expressions, ok := raw["expr"].([]any)
	if !ok {
		return 2, "unknown", ""
	}
	for _, expression := range expressions {
		object, ok := expression.(map[string]any)
		if !ok || len(object) != 1 {
			verdict = "unknown"
			continue
		}
		for kind, value := range object {
			switch kind {
			case "match":
				match, _ := value.(map[string]any)
				state := echoMatch(match, ipv6)
				if state == 0 {
					return 0, "", ""
				}
				if state == 2 {
					condition = 2
				}
			case "counter", "log":
			case "accept", "drop", "reject", "return", "jump", "goto":
				if verdict != "" {
					verdict = "unknown"
					continue
				}
				verdict = kind
				if destination, ok := value.(map[string]any); ok {
					target, _ = destination["target"].(string)
				}
			default:
				verdict = "unknown"
			}
		}
	}
	return
}

func loadedEchoState(output string, ipv6 bool) int {
	objects, err := decodeRules(output)
	if err != nil {
		return echoUnknown
	}
	rules := make(map[string][]map[string]any)
	chains := make(map[string]map[string]any)
	for _, object := range objects {
		for kind, raw := range object {
			body, ok := raw.(map[string]any)
			if !ok || body["family"] != "inet" || body["table"] != "snail" {
				continue
			}
			if kind == "chain" {
				name, _ := body["name"].(string)
				chains[name] = body
			} else if kind == "rule" {
				name, _ := body["chain"].(string)
				rules[name] = append(rules[name], body)
			}
		}
	}
	// Additional input hooks in the same table can change the final verdict.
	for name, chain := range chains {
		if chain["hook"] == "input" && name != "input" {
			return echoUnknown
		}
	}
	memo, visiting := make(map[string]int), make(map[string]bool)
	var evaluate func(string, int) int
	evaluate = func(name string, depth int) int {
		if depth > 16 || chains[name] == nil || visiting[name] {
			return echoUnknown
		}
		if state, ok := memo[name]; ok {
			return state
		}
		visiting[name] = true
		result := echoReturn
		for i := len(rules[name]) - 1; i >= 0; i-- {
			condition, verdict, target := echoRule(rules[name][i], ipv6)
			if condition == 0 || verdict == "" {
				continue
			}
			hit := echoUnknown
			switch verdict {
			case "accept":
				hit = echoAllow
			case "drop", "reject":
				hit = echoBlock
			case "return":
				hit = echoReturn
			case "jump", "goto":
				hit = evaluate(target, depth+1)
				if verdict == "jump" && hit&echoReturn != 0 {
					hit = hit&^echoReturn | result
				}
			}
			if condition == 1 {
				result = hit
			} else {
				result |= hit
			}
		}
		visiting[name] = false
		memo[name] = result
		return result
	}
	state := evaluate("input", 0)
	if state&echoReturn != 0 {
		fallback := echoUnknown
		switch chains["input"]["policy"] {
		case "accept":
			fallback = echoAllow
		case "drop":
			fallback = echoBlock
		}
		state = state&^echoReturn | fallback
	}
	return state
}

func echoStateFields(installed bool, live liveRules) []ui.CardField {
	fields := []ui.CardField{{Label: "ICMP 回显"}, {Label: "ICMPv6 回显"}}
	for i := range fields {
		switch {
		case !installed:
			fields[i].Value = "无法检测"
		case live.err != nil:
			fields[i].Value = "检测失败"
		case !live.inputActive:
			fields[i].Value = "未启用本工具过滤"
		default:
			switch loadedEchoState(live.json, i == 1) {
			case echoAllow:
				fields[i].Value = ui.PrimaryText("已允许")
			case echoBlock:
				fields[i].Value = ui.PrimaryText("已拦截")
			case echoAllow | echoBlock:
				fields[i].Value = "按规则放行"
			default:
				fields[i].Value = "无法判断"
			}
		}
	}
	return fields
}
