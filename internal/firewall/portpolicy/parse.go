package portpolicy

import (
	"fmt"
	"sort"
	"strings"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/firewall/internal/nftjson"
)

type livePortRule struct {
	families, protocols      int // IPv4/TCP = 1, IPv6/UDP = 2.
	sources, dests           []string
	ports, sourcePorts       configuration.PortIntervals
	hasPorts, hasSourcePorts bool
	hasProtocol              bool
	iif, oif                 string
	verdict, target, id      string
}

func (rule livePortRule) matchesPorts() configuration.PortIntervals {
	if !rule.hasPorts {
		return configuration.AllPorts()
	}
	return rule.ports
}

func (rule livePortRule) unconditional() bool {
	return rule.families == 3 && rule.protocols == 3 && rule.sources == nil && rule.dests == nil && rule.iif == "" && rule.oif == "" && !rule.hasPorts && !rule.hasSourcePorts
}

func (rule livePortRule) canMatchBusiness() bool {
	return rule.families != 0 && rule.protocols != 0 && (rule.sources == nil || len(rule.sources) > 0) && (!rule.hasPorts || len(rule.ports) > 0) && (!rule.hasSourcePorts || len(rule.sourcePorts) > 0)
}

func (parser livePortParser) mask(value any, family bool) (int, error) {
	values, err := nftjson.Values(value, parser.sets, 0)
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

func (parser livePortParser) sources(value any, family int, label string) ([]string, error) {
	values, err := nftjson.Values(value, parser.sets, 0)
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
			length, valid := nftjson.Int(prefix["len"])
			if !ok || !valid {
				return nil, fmt.Errorf("%s地址表达式无法识别", label)
			}
			raw = fmt.Sprintf("%v/%d", prefix["addr"], length)
		} else {
			return nil, fmt.Errorf("%s地址表达式无法识别", label)
		}
		normalized, err := configuration.NormalizeSource(raw)
		if err != nil {
			return nil, err
		}
		address := configuration.SourcePrefix(normalized).Addr()
		if address.Is4() != (family == 1) {
			return nil, fmt.Errorf("%s地址与协议族不一致", label)
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
					key, _ := meta["key"].(string)
					switch key {
					case "iifname", "oifname":
						name, ok := match["right"].(string)
						if !ok || strings.TrimSpace(name) == "" {
							return rule, fmt.Errorf("网卡条件无法识别")
						}
						if key == "iifname" {
							if rule.iif != "" && rule.iif != name {
								return rule, fmt.Errorf("含多个来源网卡条件")
							}
							rule.iif = name
						} else if rule.oif != "" && rule.oif != name {
							return rule, fmt.Errorf("含多个目标网卡条件")
						} else {
							rule.oif = name
						}
					case "nfproto", "l4proto":
						mask, err := parser.mask(match["right"], key == "nfproto")
						if err != nil {
							return rule, err
						}
						if key == "nfproto" {
							rule.families &= mask
						} else {
							rule.protocols &= mask
							rule.hasProtocol = true
						}
					default:
						return rule, fmt.Errorf("含接口或其他额外匹配条件")
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
								ports = configuration.IntersectPorts(rule.ports, ports)
							}
							rule.ports, rule.hasPorts = ports, true
						} else {
							if rule.hasSourcePorts {
								ports = configuration.IntersectPorts(rule.sourcePorts, ports)
							}
							rule.sourcePorts, rule.hasSourcePorts = ports, true
						}
					} else if (protocol == "ip" || protocol == "ip6") && (field == "saddr" || field == "daddr") {
						family := 1
						if protocol == "ip6" {
							family = 2
						}
						rule.families &= family
						label := "来源"
						if field == "daddr" {
							label = "目标"
						}
						addresses, err := parser.sources(match["right"], family, label)
						if err != nil {
							return rule, err
						}
						if field == "saddr" {
							if rule.sources != nil {
								return rule, fmt.Errorf("含多个来源条件")
							}
							rule.sources = addresses
						} else if rule.dests != nil {
							return rule, fmt.Errorf("含多个目标条件")
						} else {
							rule.dests = addresses
						}
					} else {
						return rule, fmt.Errorf("含标志位或其他额外条件")
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
			states, err := nftjson.Values(match["right"], parser.sets, 0)
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
