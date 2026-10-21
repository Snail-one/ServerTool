package firewall

import (
	"fmt"
	"sort"
	"strings"
)

type livePortRule struct {
	families, protocols      int // IPv4/TCP = 1, IPv6/UDP = 2.
	sources                  []string
	ports, sourcePorts       portIntervals
	hasPorts, hasSourcePorts bool
	hasProtocol              bool
	verdict, target, id      string
}

func (rule livePortRule) matchesPorts() portIntervals {
	if !rule.hasPorts {
		return allPorts()
	}
	return rule.ports
}

func (rule livePortRule) unconditional() bool {
	return rule.families == 3 && rule.protocols == 3 && rule.sources == nil && !rule.hasPorts && !rule.hasSourcePorts
}

func (rule livePortRule) canMatchBusiness() bool {
	return rule.families != 0 && rule.protocols != 0 && (rule.sources == nil || len(rule.sources) > 0) && (!rule.hasPorts || len(rule.ports) > 0) && (!rule.hasSourcePorts || len(rule.sourcePorts) > 0)
}

func (parser livePortParser) mask(value any, family bool) (int, error) {
	values, err := parser.values(value, 0)
	if err != nil {
		return 0, err
	}
	mask := 0
	for _, value := range values {
		switch fmt.Sprint(value) {
		case "ipv4", "2":
			if !family {
				return 0, fmt.Errorf("传输层协议无法识别")
			}
			mask |= 1
		case "ipv6", "10":
			if !family {
				return 0, fmt.Errorf("传输层协议无法识别")
			}
			mask |= 2
		case "tcp", "6":
			if family {
				return 0, fmt.Errorf("IP 协议族无法识别")
			}
			mask |= 1
		case "udp", "17":
			if family {
				return 0, fmt.Errorf("IP 协议族无法识别")
			}
			mask |= 2
		default:
			return 0, fmt.Errorf("协议条件无法识别: %v", value)
		}
	}
	return mask, nil
}

func (parser livePortParser) sources(value any, family int) ([]string, error) {
	values, err := parser.values(value, 0)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		var raw string
		if text, ok := value.(string); ok {
			raw = text
		} else if object, ok := value.(map[string]any); ok {
			prefix, ok := object["prefix"].(map[string]any)
			length, valid := nftInt(prefix["len"])
			if !ok || !valid {
				return nil, fmt.Errorf("来源地址表达式无法识别")
			}
			raw = fmt.Sprintf("%v/%d", prefix["addr"], length)
		} else {
			return nil, fmt.Errorf("来源地址表达式无法识别")
		}
		normalized, err := normalizeSource(raw)
		if err != nil {
			return nil, err
		}
		address := sourcePrefix(normalized).Addr()
		if address.Is4() != (family == 1) {
			return nil, fmt.Errorf("来源地址与协议族不一致")
		}
		result = append(result, normalized)
	}
	sort.Strings(result)
	return result, nil
}

func (parser livePortParser) rule(raw map[string]any) (livePortRule, error) {
	rule := livePortRule{families: 3, protocols: 3}
	if comment, ok := raw["comment"].(string); ok && strings.HasPrefix(comment, "snail-rule:") {
		rule.id = strings.TrimPrefix(comment, "snail-rule:")
	}
	if rule.id == "" {
		rule.id = fmt.Sprintf("%v", raw["handle"])
	}
	expressions, ok := raw["expr"].([]any)
	if !ok {
		return rule, fmt.Errorf("规则缺少表达式")
	}
	for _, expression := range expressions {
		object, ok := expression.(map[string]any)
		if !ok || len(object) != 1 {
			return rule, fmt.Errorf("规则表达式无效")
		}
		for kind, value := range object {
			switch kind {
			case "counter":
				continue
			case "accept", "drop", "return", "jump", "goto":
				if rule.verdict != "" {
					return rule, fmt.Errorf("存在多个终止动作")
				}
				rule.verdict = kind
				if kind == "jump" || kind == "goto" {
					target, _ := value.(map[string]any)
					rule.target, _ = target["target"].(string)
					if rule.target == "" {
						return rule, fmt.Errorf("跳转目标缺失")
					}
				}
			case "match":
				match, ok := value.(map[string]any)
				if !ok || (match["op"] != "==" && match["op"] != "in") {
					return rule, fmt.Errorf("不支持的匹配运算")
				}
				left, _ := match["left"].(map[string]any)
				if meta, ok := left["meta"].(map[string]any); ok {
					family := meta["key"] == "nfproto"
					if !family && meta["key"] != "l4proto" {
						return rule, fmt.Errorf("含接口或其他额外匹配条件")
					}
					mask, err := parser.mask(match["right"], family)
					if err != nil {
						return rule, err
					}
					if family {
						rule.families &= mask
					} else {
						rule.protocols &= mask
						rule.hasProtocol = true
					}
				} else if payload, ok := left["payload"].(map[string]any); ok {
					protocol, field := payload["protocol"], payload["field"]
					if (protocol == "tcp" || protocol == "udp") && (field == "dport" || field == "sport") {
						rule.hasProtocol = true
						mask := 1
						if protocol == "udp" {
							mask = 2
						}
						rule.protocols &= mask
						ports, err := parser.ports(match["right"])
						if err != nil {
							return rule, err
						}
						if field == "dport" {
							if rule.hasPorts {
								ports = intersectPorts(rule.ports, ports)
							}
							rule.ports, rule.hasPorts = ports, true
						} else {
							if rule.hasSourcePorts {
								ports = intersectPorts(rule.sourcePorts, ports)
							}
							rule.sourcePorts, rule.hasSourcePorts = ports, true
						}
					} else if (protocol == "ip" || protocol == "ip6") && field == "saddr" {
						if rule.sources != nil {
							return rule, fmt.Errorf("含多个来源条件")
						}
						family := 1
						if protocol == "ip6" {
							family = 2
						}
						rule.families &= family
						sources, err := parser.sources(match["right"], family)
						if err != nil {
							return rule, err
						}
						rule.sources = sources
					} else {
						return rule, fmt.Errorf("含目标地址、标志位或其他额外条件")
					}
				} else {
					return rule, fmt.Errorf("不支持的匹配表达式")
				}
			default:
				return rule, fmt.Errorf("不支持的规则动作: %s", kind)
			}
		}
	}
	return rule, nil
}

func (parser livePortParser) infrastructure(raw map[string]any) string {
	expressions, _ := raw["expr"].([]any)
	for _, expression := range expressions {
		object, _ := expression.(map[string]any)
		match, _ := object["match"].(map[string]any)
		if match["op"] != "==" && match["op"] != "in" {
			continue
		}
		left, _ := match["left"].(map[string]any)
		if meta, ok := left["meta"].(map[string]any); ok && meta["key"] == "iifname" && match["right"] == "lo" {
			return "回环接口流量"
		}
		if payload, ok := left["payload"].(map[string]any); ok && (payload["protocol"] == "icmpv6" || payload["protocol"] == "icmp") {
			return "ICMP / ICMPv6 控制流量"
		}
		if ct, ok := left["ct"].(map[string]any); ok && ct["key"] == "state" {
			states, err := parser.values(match["right"], 0)
			valid := err == nil && len(states) > 0
			for _, state := range states {
				valid = valid && (state == "established" || state == "related")
			}
			if valid {
				return "已建立连接及关联流量"
			}
		}
	}
	return ""
}
