package firewall

import (
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
)

type policyList struct {
	name, label string
	family      int // 0 for ports, 4 or 6 for source addresses
	defaults    string
}

var policyLists = []policyList{
	{name: "snail_public_ipv4_tcp_ports", label: "公网 IPv4 TCP"},
	{name: "snail_public_ipv4_udp_ports", label: "公网 IPv4 UDP"},
	{name: "snail_public_ipv6_tcp_ports", label: "公网 IPv6 TCP"},
	{name: "snail_public_ipv6_udp_ports", label: "公网 IPv6 UDP"},
	{name: "snail_private_ipv4_tcp_ports", label: "内网 IPv4 TCP"},
	{name: "snail_private_ipv4_udp_ports", label: "内网 IPv4 UDP"},
	{name: "snail_private_ipv6_tcp_ports", label: "内网 IPv6 TCP"},
	{name: "snail_private_ipv6_udp_ports", label: "内网 IPv6 UDP"},
	{name: "snail_private_ipv4", label: "IPv4 内网来源名单", family: 4, defaults: "10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16"},
	{name: "snail_private_ipv6", label: "IPv6 内网来源名单", family: 6, defaults: "fc00::/7, fe80::/10"},
}

// Definitions are found through the tokenizer so comments, multiline sets and
// quoted strings cannot be mistaken for active definitions. Only the set body
// is replaced; the surrounding configuration and comments remain intact.
func policyDefinition(content, name string) (int, int, string, error) {
	tokens, err := configTokens(content)
	if err != nil {
		return 0, 0, "", err
	}
	start, end := -1, -1
	depth := 0
	var values []string
	for i := 0; i < len(tokens); i++ {
		if tokens[i].text == "define" && i+1 < len(tokens) && tokens[i+1].text == name {
			if start >= 0 || depth != 0 || i+3 >= len(tokens) || tokens[i+2].text != "=" || tokens[i+3].text != "{" {
				return 0, 0, "", fmt.Errorf("%s 定义重复或格式不受支持，请通过编辑配置文件检查", name)
			}
			close, err := closingBrace(tokens, i+3)
			if err != nil {
				return 0, 0, "", err
			}
			start, end = tokens[i+3].end, tokens[close].start
			for _, token := range tokens[i+4 : close] {
				values = append(values, token.text)
			}
			i = close
			continue
		}
		switch tokens[i].text {
		case "{":
			depth++
		case "}":
			depth--
		}
	}
	if start < 0 {
		return 0, 0, "", fmt.Errorf("缺少 %s 集合，请先生成基础配置，或通过编辑配置文件补全", name)
	}
	return start, end, strings.Join(values, " "), nil
}

func parsePolicyPorts(raw string) (portIntervals, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var ports portIntervals
	for _, part := range strings.Split(strings.ReplaceAll(raw, "，", ","), ",") {
		_, first, last, err := parseRulePorts(part)
		if err != nil {
			return nil, err
		}
		ports = append(ports, portInterval{first, last})
	}
	return unionPorts(ports), nil
}

func policyPortsText(ports portIntervals) string {
	if len(ports) == 0 {
		return ""
	}
	var parts []string
	for _, port := range unionPorts(ports) {
		part := strconv.Itoa(port.first)
		if port.first != port.last {
			part += "-" + strconv.Itoa(port.last)
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ", ")
}

func parsePolicySources(raw string, family int) ([]string, error) {
	var prefixes []netip.Prefix
	for _, part := range strings.Split(strings.ReplaceAll(raw, "，", ","), ",") {
		part = strings.TrimSpace(part)
		var prefix netip.Prefix
		if addr, err := netip.ParseAddr(part); err == nil && addr.Zone() == "" {
			prefix = netip.PrefixFrom(addr, addr.BitLen())
		} else {
			prefix, err = netip.ParsePrefix(part)
			if err != nil {
				return nil, fmt.Errorf("无效的 IPv%d 地址或 CIDR：%q", family, part)
			}
		}
		if prefix.Addr().Is4In6() || prefix.Addr().Is4() != (family == 4) {
			return nil, fmt.Errorf("名单仅接受 IPv%d 地址或 CIDR，不接受混合地址族或 IPv4 映射地址", family)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	sort.Slice(prefixes, func(i, j int) bool {
		if prefixes[i].Addr() == prefixes[j].Addr() {
			return prefixes[i].Bits() < prefixes[j].Bits()
		}
		return prefixes[i].Addr().Less(prefixes[j].Addr())
	})
	var result []string
	var previous netip.Prefix
	for _, prefix := range prefixes {
		if previous.IsValid() && previous.Contains(prefix.Addr()) {
			continue
		}
		previous = prefix
		value := prefix.String()
		if prefix.Bits() == prefix.Addr().BitLen() {
			value = prefix.Addr().String()
		}
		result = append(result, value)
	}
	return result, nil
}

func readPolicyValue(content string, list policyList) (string, error) {
	_, _, raw, err := policyDefinition(content, list.name)
	if err != nil {
		return "", err
	}
	if list.family != 0 {
		sources, err := parsePolicySources(raw, list.family)
		return strings.Join(sources, ", "), err
	}
	business, err := policyBusinessTerms(raw, list.label)
	if err != nil {
		return "", err
	}
	ports, err := parsePolicyPorts(strings.Join(business, ","))
	return policyPortsText(ports), err
}

func policyBusinessTerms(raw, label string) ([]string, error) {
	var business []string
	references := 0
	for _, part := range strings.Split(raw, ",") {
		if strings.TrimSpace(part) == "" {
			return nil, fmt.Errorf("%s 含空的端口条目，请检查配置", label)
		}
		if strings.TrimSpace(part) == "$snail_ssh_port" {
			references++
		} else {
			if strings.Contains(part, "$snail_ssh_port") {
				return nil, fmt.Errorf("%s 的 SSH 集合引用异常，请先修正配置", label)
			}
			business = append(business, part)
		}
	}
	if references != 1 {
		return nil, fmt.Errorf("%s 缺少唯一的 SSH 集合引用，请先修正配置", label)
	}
	return business, nil
}

func withPolicyValue(content string, list policyList, value string) (string, error) {
	if _, err := readPolicyValue(content, list); err != nil {
		return "", err
	}
	start, end, _, err := policyDefinition(content, list.name)
	if err != nil {
		return "", err
	}
	if list.family == 0 {
		ports, err := parsePolicyPorts(value)
		if err != nil {
			return "", err
		}
		value = "$snail_ssh_port"
		if len(ports) > 0 {
			value += ", " + policyPortsText(ports)
		}
	} else {
		sources, err := parsePolicySources(value, list.family)
		if err != nil {
			return "", err
		}
		value = strings.Join(sources, ", ")
	}
	return content[:start] + " " + value + " " + content[end:], nil
}

func validatePolicyConfig(content string) error {
	if !hasManagedConfig(content) {
		return fmt.Errorf("请先选择 6 生成本工具防火墙配置")
	}
	if _, _, _, err := inputChainBounds(content); err != nil {
		return err
	}
	for _, list := range policyLists {
		if _, err := readPolicyValue(content, list); err != nil {
			return fmt.Errorf("%s：%w", list.label, err)
		}
	}
	ssh := sshDefinitionLine.FindAllStringSubmatch(content, -1)
	if len(ssh) != 1 {
		return fmt.Errorf("SSH 端口集合缺失、重复或格式异常，请检查配置")
	}
	if _, err := parsePolicyPorts(ssh[0][1]); err != nil {
		return fmt.Errorf("SSH 端口集合无效: %w", err)
	}
	return nil
}
