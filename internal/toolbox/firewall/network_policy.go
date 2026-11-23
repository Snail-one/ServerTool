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
	keepSSH     bool
	namedSet    bool
}

type policySession struct {
	title   string
	lists   []policyList
	block   ruleBlock
	forward bool
}

var activeSession *policySession

func sessionTitle() string {
	if activeSession != nil && activeSession.title != "" {
		return activeSession.title
	}
	return "访问规则管理"
}

func sessionLists() []policyList {
	if activeSession != nil && len(activeSession.lists) > 0 {
		return activeSession.lists
	}
	return policyLists
}

func sessionDraftName() string {
	if activeSession != nil && activeSession.forward {
		return "转发规则草稿"
	}
	return "访问规则草稿"
}

func useForwardSession() func() {
	session := policySession{
		title:   "转发规则管理",
		lists:   forwardPolicyLists,
		block:   forwardRuleBlock,
		forward: true,
	}
	activeSession = &session
	return func() { activeSession = nil }
}

var policyLists = []policyList{
	{name: "snail_public_ipv4_tcp_ports", label: "公网 IPv4 TCP", namedSet: true},
	{name: "snail_public_ipv4_udp_ports", label: "公网 IPv4 UDP", namedSet: true},
	{name: "snail_public_ipv6_tcp_ports", label: "公网 IPv6 TCP", namedSet: true},
	{name: "snail_public_ipv6_udp_ports", label: "公网 IPv6 UDP", namedSet: true},
	{name: "snail_private_ipv4_tcp_ports", label: "内网 IPv4 TCP", namedSet: true},
	{name: "snail_private_ipv4_udp_ports", label: "内网 IPv4 UDP", namedSet: true},
	{name: "snail_private_ipv6_tcp_ports", label: "内网 IPv6 TCP", namedSet: true},
	{name: "snail_private_ipv6_udp_ports", label: "内网 IPv6 UDP", namedSet: true},
	{name: "snail_private_ipv4", label: "IPv4 内网范围", family: 4, defaults: "172.16.0.0/12, 192.168.0.0/16"},
	{name: "snail_private_ipv6", label: "IPv6 内网范围", family: 6, defaults: "fc00::/7, fe80::/10"},
}

var forwardPolicyLists = []policyList{
	{name: "snail_forward_public_ipv4_tcp_ports", label: "公网 IPv4 TCP", namedSet: true},
	{name: "snail_forward_public_ipv4_udp_ports", label: "公网 IPv4 UDP", namedSet: true},
	{name: "snail_forward_public_ipv6_tcp_ports", label: "公网 IPv6 TCP", namedSet: true},
	{name: "snail_forward_public_ipv6_udp_ports", label: "公网 IPv6 UDP", namedSet: true},
	{name: "snail_forward_private_ipv4_tcp_ports", label: "内网 IPv4 TCP", namedSet: true},
	{name: "snail_forward_private_ipv4_udp_ports", label: "内网 IPv4 UDP", namedSet: true},
	{name: "snail_forward_private_ipv6_tcp_ports", label: "内网 IPv6 TCP", namedSet: true},
	{name: "snail_forward_private_ipv6_udp_ports", label: "内网 IPv6 UDP", namedSet: true},
	{name: "snail_forward_private_ipv4", label: "IPv4 内网范围", family: 4, defaults: "172.16.0.0/12, 192.168.0.0/16"},
	{name: "snail_forward_private_ipv6", label: "IPv6 内网范围", family: 6, defaults: "fc00::/7, fe80::/10"},
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
	raw, err := policyRawValue(content, list)
	if err != nil {
		return "", err
	}
	if list.family != 0 {
		sources, err := parsePolicySources(raw, list.family)
		return strings.Join(sources, ", "), err
	}
	business, err := policyBusinessTerms(raw, list.label, list.keepSSH)
	if err != nil {
		return "", err
	}
	ports, err := parsePolicyPorts(strings.Join(business, ","))
	return policyPortsText(ports), err
}

func policyRawValue(content string, list policyList) (string, error) {
	if list.namedSet {
		return namedSetValue(content, list.name)
	}
	_, _, raw, err := policyDefinition(content, list.name)
	return raw, err
}

func policyBusinessTerms(raw, label string, keepSSH bool) ([]string, error) {
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

func withPolicyValue(content string, list policyList, value string) (string, error) {
	if _, err := readPolicyValue(content, list); err != nil {
		return "", err
	}
	if list.family == 0 {
		ports, err := parsePolicyPorts(value)
		if err != nil {
			return "", err
		}
		value = policyPortsText(ports)
		if list.keepSSH {
			value = "$snail_ssh_port"
			if portsText := policyPortsText(ports); portsText != "" {
				value += ", " + portsText
			}
		}
	} else {
		sources, err := parsePolicySources(value, list.family)
		if err != nil {
			return "", err
		}
		value = strings.Join(sources, ", ")
	}
	if list.namedSet {
		return withNamedSetElements(content, list.name, value)
	}
	start, end, _, err := policyDefinition(content, list.name)
	if err != nil {
		return "", err
	}
	return content[:start] + " " + value + " " + content[end:], nil
}

// Named sets stay valid when they have no elements, so an empty forward port
// list drops new connections instead of failing nft syntax.
func namedSetValue(content, name string) (string, error) {
	_, _, elementsStart, elementsEnd, err := namedSetBounds(content, name)
	if err != nil {
		return "", err
	}
	if elementsStart < 0 {
		return "", nil
	}
	tokens, err := configTokens(content[elementsStart:elementsEnd])
	if err != nil {
		return "", err
	}
	var values []string
	for _, token := range tokens {
		values = append(values, token.text)
	}
	return strings.Join(values, " "), nil
}

func namedSetBounds(content, name string) (bodyEnd, elementsToken, elementsStart, elementsEnd int, err error) {
	tokens, err := configTokens(content)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	found := false
	elementsToken, elementsStart, elementsEnd = -1, -1, -1
	for i := 0; i+2 < len(tokens); i++ {
		if tokens[i].text != "set" || tokens[i+1].text != name || tokens[i+2].text != "{" {
			continue
		}
		if found {
			return 0, 0, 0, 0, fmt.Errorf("%s 集合重复或格式不受支持，请通过编辑配置文件检查", name)
		}
		close, err := closingBrace(tokens, i+2)
		if err != nil {
			return 0, 0, 0, 0, err
		}
		found = true
		bodyEnd = tokens[close].start
		for j := i + 3; j+2 < close; j++ {
			if tokens[j].text == "elements" && tokens[j+1].text == "=" && tokens[j+2].text == "{" {
				if elementsStart >= 0 {
					return 0, 0, 0, 0, fmt.Errorf("%s 集合的 elements 重复，请通过编辑配置文件检查", name)
				}
				end, err := closingBrace(tokens, j+2)
				if err != nil {
					return 0, 0, 0, 0, err
				}
				elementsToken = tokens[j].start
				elementsStart, elementsEnd = tokens[j+2].end, tokens[end].start
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

func withNamedSetElements(content, name, value string) (string, error) {
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

func validatePolicyConfig(content string) error {
	if !hasManagedConfig(content) {
		return fmt.Errorf("请先选择 8 生成本工具防火墙配置")
	}
	if _, _, _, err := inputChainBounds(content); err != nil {
		return err
	}
	for _, list := range policyLists {
		if _, err := readPolicyValue(content, list); err != nil {
			return fmt.Errorf("%s：%w", list.label, err)
		}
	}
	if _, err := readSSHPorts(content); err != nil {
		return err
	}
	return nil
}

func validateForwardConfig(content string) error {
	if !hasManagedConfig(content) {
		return fmt.Errorf("请先选择 8 生成本工具防火墙配置")
	}
	if _, err := managedRulesIn(content, forwardRuleBlock); err != nil {
		return fmt.Errorf("当前配置没有可用的转发链，请先选择 8 重建基础配置: %w", err)
	}
	for _, list := range forwardPolicyLists {
		if _, err := readPolicyValue(content, list); err != nil {
			return fmt.Errorf("%s：%w", list.label, err)
		}
	}
	return nil
}

// Build the complete edit in memory. A failure in any selected list returns
// no replacement, so a batch never leaves a partially updated parent draft.
func withPolicyChanges(content string, indices []int, operation, value string, editIndex int) (string, error) {
	updated := content
	lists := sessionLists()
	for _, index := range indices {
		list := lists[index]
		current, err := readPolicyValue(updated, list)
		if err != nil {
			return "", err
		}
		next, err := changePolicyValue(list, current, operation, value, editIndex)
		if err != nil {
			return "", err
		}
		updated, err = withPolicyValue(updated, list, next)
		if err != nil {
			return "", err
		}
	}
	return updated, nil
}

func changePolicyValue(list policyList, current, operation, value string, editIndex int) (string, error) {
	if operation == "reset" {
		return list.defaults, nil
	}
	if list.family == 0 {
		old, err := parsePolicyPorts(current)
		if err != nil {
			return "", err
		}
		ports, err := parsePolicyPorts(value)
		if err != nil {
			return "", err
		}
		switch operation {
		case "add":
			ports = unionPorts(append(old, ports...))
		case "remove":
			ports = subtractPorts(old, ports)
		}
		return policyPortsText(ports), nil
	}
	entries, err := parsePolicySources(value, list.family)
	if err != nil {
		return "", err
	}
	old := strings.Split(current, ", ")
	switch operation {
	case "add":
		entries = append(old, entries...)
	case "remove":
		removed := make(map[string]bool)
		for _, entry := range entries {
			removed[entry] = true
		}
		entries = nil
		for _, entry := range old {
			if !removed[entry] {
				entries = append(entries, entry)
			}
		}
		if len(entries) == 0 {
			return "", fmt.Errorf("内网 IP 范围至少保留一项；可替换整个范围或恢复默认")
		}
	case "edit":
		if len(entries) != 1 || editIndex < 0 || editIndex >= len(old) {
			return "", fmt.Errorf("编辑单个条目时请只输入一个地址或 CIDR")
		}
		old[editIndex] = entries[0]
		entries = old
	}
	entries, err = parsePolicySources(strings.Join(entries, ", "), list.family)
	return strings.Join(entries, ", "), err
}
