package rules

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/firewall/settings/sshaccess"
	"snail_tool/internal/ui"
)

type PolicyList struct {
	Name, Label string
	Family      int // 0 for ports, 4 or 6 for source addresses
	Defaults    string
	KeepSSH     bool
	NamedSet    bool
}

type Session struct {
	title, label string
	lists        []PolicyList
	block        Block
	forward      bool
	summary      func(string) []ui.CardField
	migrate      func(string, string) (string, error)
	immediate    ImmediateActions
	options      Options
}

type Options struct {
	Store            configuration.Store
	IsRoot           func() bool
	HasManagedConfig func(string) bool
}

type ImmediateActions struct {
	Fields func(string) []ui.CardField
	Print  func()
	Choose func(*ui.UI, string) (func() error, string)
}

func NewAccess(options Options, title, label string, lists []PolicyList, summary func(string) []ui.CardField, migrate func(string, string) (string, error)) Session {
	return Session{title: title, label: label, lists: lists, block: AccessBlock, summary: summary, migrate: migrate, options: options}
}

func NewForward(options Options, title, label string, lists []PolicyList, summary func(string) []ui.CardField, actions ImmediateActions) Session {
	return Session{title: title, label: label, lists: lists, block: ForwardBlock, forward: true, summary: summary, immediate: actions, options: options}
}

// Definitions are found through the tokenizer so comments, multiline sets and
// quoted strings cannot be mistaken for active definitions. Only the set body
// is replaced; the surrounding configuration and comments remain intact.
func PolicyDefinition(content, name string) (int, int, string, error) {
	tokens, err := configuration.Tokens(content)
	if err != nil {
		return 0, 0, "", err
	}
	start, end := -1, -1
	depth := 0
	var values []string
	for i := 0; i < len(tokens); i++ {
		if tokens[i].Text == "define" && i+1 < len(tokens) && tokens[i+1].Text == name {
			if start >= 0 || depth != 0 || i+3 >= len(tokens) || tokens[i+2].Text != "=" || tokens[i+3].Text != "{" {
				return 0, 0, "", fmt.Errorf("%s 定义重复或格式不受支持，请通过编辑配置文件检查", name)
			}
			close, err := configuration.ClosingBrace(tokens, i+3)
			if err != nil {
				return 0, 0, "", err
			}
			start, end = tokens[i+3].End, tokens[close].Start
			for _, token := range tokens[i+4 : close] {
				values = append(values, token.Text)
			}
			i = close
			continue
		}
		switch tokens[i].Text {
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

func ParsePolicySources(raw string, family int) ([]string, error) {
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

func ReadPolicyValue(content string, list PolicyList) (string, error) {
	raw, err := PolicyRawValue(content, list)
	if err != nil {
		return "", err
	}
	if list.Family != 0 {
		sources, err := ParsePolicySources(raw, list.Family)
		return strings.Join(sources, ", "), err
	}
	business, err := configuration.BusinessPortTerms(raw, list.Label, list.KeepSSH)
	if err != nil {
		return "", err
	}
	ports, err := configuration.ParsePorts(strings.Join(business, ","))
	return configuration.PortsText(ports), err
}

func PolicyRawValue(content string, list PolicyList) (string, error) {
	if list.NamedSet {
		return configuration.NamedSetValue(content, list.Name)
	}
	_, _, raw, err := PolicyDefinition(content, list.Name)
	return raw, err
}

func WithPolicyValue(content string, list PolicyList, value string) (string, error) {
	if _, err := ReadPolicyValue(content, list); err != nil {
		return "", err
	}
	if list.Family == 0 {
		ports, err := configuration.ParsePorts(value)
		if err != nil {
			return "", err
		}
		value = configuration.PortsText(ports)
		if list.KeepSSH {
			value = "$snail_ssh_port"
			if portsText := configuration.PortsText(ports); portsText != "" {
				value += ", " + portsText
			}
		}
	} else {
		sources, err := ParsePolicySources(value, list.Family)
		if err != nil {
			return "", err
		}
		value = strings.Join(sources, ", ")
	}
	if list.NamedSet {
		return configuration.ReplaceNamedSetElements(content, list.Name, value)
	}
	start, end, _, err := PolicyDefinition(content, list.Name)
	if err != nil {
		return "", err
	}
	return content[:start] + " " + value + " " + content[end:], nil
}

func (session Session) ValidateConfig(content string) error {
	if session.forward {
		return session.validateForwardConfig(content)
	}
	if !session.options.HasManagedConfig(content) {
		return fmt.Errorf("请先选择 d 生成本工具防火墙配置")
	}
	if _, _, _, err := configuration.InputChainBounds(content); err != nil {
		return err
	}
	for _, list := range session.lists {
		if _, err := ReadPolicyValue(content, list); err != nil {
			return fmt.Errorf("%s：%w", list.Label, err)
		}
	}
	if _, err := sshaccess.ReadPorts(content); err != nil {
		return err
	}
	return nil
}

func (session Session) validateForwardConfig(content string) error {
	if !session.options.HasManagedConfig(content) {
		return fmt.Errorf("请先选择 d 生成本工具防火墙配置")
	}
	if _, err := ManagedRulesIn(content, ForwardBlock); err != nil {
		return fmt.Errorf("当前配置没有可用的转发链，请先选择 d 重建基础配置: %w", err)
	}
	for _, list := range session.lists {
		if _, err := ReadPolicyValue(content, list); err != nil {
			return fmt.Errorf("%s：%w", list.Label, err)
		}
	}
	return nil
}

// Build the complete edit in memory. A failure in any selected list returns
// no replacement, so a batch never leaves a partially updated parent draft.
func (session Session) WithPolicyChanges(content string, indices []int, operation, value string, editIndex int) (string, error) {
	updated := content
	lists := session.lists
	for _, index := range indices {
		list := lists[index]
		current, err := ReadPolicyValue(updated, list)
		if err != nil {
			return "", err
		}
		next, err := ChangePolicyValue(list, current, operation, value, editIndex)
		if err != nil {
			return "", err
		}
		updated, err = WithPolicyValue(updated, list, next)
		if err != nil {
			return "", err
		}
	}
	return updated, nil
}

func ChangePolicyValue(list PolicyList, current, operation, value string, editIndex int) (string, error) {
	if operation == "reset" {
		return list.Defaults, nil
	}
	if list.Family == 0 {
		old, err := configuration.ParsePorts(current)
		if err != nil {
			return "", err
		}
		ports, err := configuration.ParsePorts(value)
		if err != nil {
			return "", err
		}
		switch operation {
		case "add":
			ports = configuration.UnionPorts(append(old, ports...))
		case "remove":
			ports = configuration.SubtractPorts(old, ports)
		}
		return configuration.PortsText(ports), nil
	}
	entries, err := ParsePolicySources(value, list.Family)
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
	entries, err = ParsePolicySources(strings.Join(entries, ", "), list.Family)
	return strings.Join(entries, ", "), err
}
