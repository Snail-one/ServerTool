package configuration

import (
	"fmt"
	"strconv"
	"strings"
)

func ParsePorts(raw string) (PortIntervals, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var ports PortIntervals
	for _, part := range strings.Split(strings.ReplaceAll(raw, "，", ","), ",") {
		_, first, last, err := ParsePortRange(part)
		if err != nil {
			return nil, err
		}
		ports = append(ports, PortInterval{First: first, Last: last})
	}
	return UnionPorts(ports), nil
}

func PortsText(ports PortIntervals) string {
	if len(ports) == 0 {
		return ""
	}
	var parts []string
	for _, port := range UnionPorts(ports) {
		part := strconv.Itoa(port.First)
		if port.First != port.Last {
			part += "-" + strconv.Itoa(port.Last)
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ", ")
}

func BusinessPortTerms(raw, label string, keepSSH bool) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		if keepSSH {
			return nil, fmt.Errorf("%s 缺少唯一的 SSH 集合引用，请先修正配置", label)
		}
		return nil, nil
	}
	var business []string
	references := 0
	for _, part := range strings.Split(raw, ",") {
		if strings.TrimSpace(part) == "" {
			return nil, fmt.Errorf("%s 含空的端口条目，请检查配置", label)
		}
		if strings.TrimSpace(part) == "$snail_ssh_port" {
			references++
			if !keepSSH {
				return nil, fmt.Errorf("%s 不能引用 SSH 端口集合", label)
			}
		} else {
			if strings.Contains(part, "$snail_ssh_port") || (!keepSSH && strings.Contains(part, "$")) {
				return nil, fmt.Errorf("%s 的端口条目含不受支持的集合引用，请先修正配置", label)
			}
			business = append(business, part)
		}
	}
	if keepSSH && references != 1 {
		return nil, fmt.Errorf("%s 缺少唯一的 SSH 集合引用，请先修正配置", label)
	}
	return business, nil
}

// Named sets stay valid when they have no elements, so an empty forward port
// list drops new connections instead of failing nft syntax.
func NamedSetValue(content, name string) (string, error) {
	_, _, elementsStart, elementsEnd, err := namedSetBounds(content, name)
	if err != nil {
		return "", err
	}
	if elementsStart < 0 {
		return "", nil
	}
	tokens, err := Tokens(content[elementsStart:elementsEnd])
	if err != nil {
		return "", err
	}
	var values []string
	for _, token := range tokens {
		values = append(values, token.Text)
	}
	return strings.Join(values, " "), nil
}

func namedSetBounds(content, name string) (bodyEnd, elementsToken, elementsStart, elementsEnd int, err error) {
	tokens, err := Tokens(content)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	found := false
	elementsToken, elementsStart, elementsEnd = -1, -1, -1
	for i := 0; i+2 < len(tokens); i++ {
		if tokens[i].Text != "set" || tokens[i+1].Text != name || tokens[i+2].Text != "{" {
			continue
		}
		if found {
			return 0, 0, 0, 0, fmt.Errorf("%s 集合重复或格式不受支持，请通过编辑配置文件检查", name)
		}
		close, err := ClosingBrace(tokens, i+2)
		if err != nil {
			return 0, 0, 0, 0, err
		}
		found = true
		bodyEnd = tokens[close].Start
		for j := i + 3; j+2 < close; j++ {
			if tokens[j].Text == "elements" && tokens[j+1].Text == "=" && tokens[j+2].Text == "{" {
				if elementsStart >= 0 {
					return 0, 0, 0, 0, fmt.Errorf("%s 集合的 elements 重复，请通过编辑配置文件检查", name)
				}
				end, err := ClosingBrace(tokens, j+2)
				if err != nil {
					return 0, 0, 0, 0, err
				}
				elementsToken = tokens[j].Start
				elementsStart, elementsEnd = tokens[j+2].End, tokens[end].Start
				j = end
			}
		}
		i = close
	}
	if !found {
		return 0, 0, 0, 0, fmt.Errorf("缺少 %s 集合，请先生成基础配置，或通过编辑配置文件补全", name)
	}
	return bodyEnd, elementsToken, elementsStart, elementsEnd, nil
}

func ReplaceNamedSetElements(content, name, value string) (string, error) {
	bodyEnd, elementsToken, elementsStart, elementsEnd, err := namedSetBounds(content, name)
	if err != nil {
		return "", err
	}
	if value == "" {
		if elementsStart < 0 {
			return content, nil
		}
		line := strings.LastIndex(content[:elementsToken], "\n") + 1
		after := elementsEnd
		// elementsEnd is the offset of the closing brace; skip that brace and one newline.
		brace := strings.Index(content[elementsEnd:], "}")
		if brace < 0 {
			return "", fmt.Errorf("%s 集合的 elements 花括号未闭合", name)
		}
		after = elementsEnd + brace + 1
		if after < len(content) && content[after] == '\n' {
			after++
		}
		return content[:line] + content[after:], nil
	}
	if elementsStart >= 0 {
		return content[:elementsStart] + " " + value + " " + content[elementsEnd:], nil
	}
	insertAt := strings.LastIndex(content[:bodyEnd], "\n") + 1
	return content[:insertAt] + "        elements = { " + value + " }\n" + content[insertAt:], nil
}

type PortSet struct{ Name, Label string }

func InputPortSets(public bool) [4]PortSet {
	scope, label := "private", "内网"
	if public {
		scope, label = "public", "公网"
	}
	return [4]PortSet{
		{"snail_" + scope + "_ipv4_tcp_ports", label + " IPv4 TCP"},
		{"snail_" + scope + "_ipv4_udp_ports", label + " IPv4 UDP"},
		{"snail_" + scope + "_ipv6_tcp_ports", label + " IPv6 TCP"},
		{"snail_" + scope + "_ipv6_udp_ports", label + " IPv6 UDP"},
	}
}

func ReadPortSet(content string, set PortSet) (string, error) {
	raw, err := NamedSetValue(content, set.Name)
	if err != nil {
		return "", err
	}
	business, err := BusinessPortTerms(raw, set.Label, false)
	if err != nil {
		return "", err
	}
	ports, err := ParsePorts(strings.Join(business, ","))
	return PortsText(ports), err
}
