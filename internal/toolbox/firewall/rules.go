package firewall

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

type allowRule struct {
	ID         string `json:"id"`
	Kind       string `json:"type"`
	Family     string `json:"family,omitempty"`
	Source     string `json:"source,omitempty"`
	SourceIPv6 string `json:"source_ipv6,omitempty"`
	Ports      string `json:"ports,omitempty"`
	Protocol   string `json:"protocol"`
}

func normalizeSource(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if addr, err := netip.ParseAddr(raw); err == nil {
		if addr.Zone() != "" {
			return "", fmt.Errorf("来源地址不能包含接口区域标识")
		}
		return addr.Unmap().String(), nil
	}
	prefix, err := netip.ParsePrefix(raw)
	if err != nil {
		return "", fmt.Errorf("请输入有效的 IPv4、IPv6 或 CIDR 来源地址")
	}
	if prefix.Addr().Is4In6() {
		if prefix.Bits() < 96 {
			return "", fmt.Errorf("IPv4 映射地址的网段前缀不能小于 96")
		}
		prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
	}
	prefix = prefix.Masked()
	if prefix.Bits() == prefix.Addr().BitLen() {
		return prefix.Addr().String(), nil
	}
	return prefix.String(), nil
}

func sourcePrefix(source string) netip.Prefix {
	if source == "" {
		return netip.Prefix{}
	}
	if addr, err := netip.ParseAddr(source); err == nil {
		return netip.PrefixFrom(addr, addr.BitLen())
	}
	prefix, _ := netip.ParsePrefix(source)
	return prefix.Masked()
}

func sourceRange(source string) string {
	if source == "" {
		return "任意来源（IPv4 / IPv6）"
	}
	prefix := sourcePrefix(source)
	first := prefix.Addr()
	if prefix.Bits() == first.BitLen() {
		return first.String()
	}
	var last netip.Addr
	if first.Is4() {
		value := first.As4()
		for bit := prefix.Bits(); bit < 32; bit++ {
			value[bit/8] |= 1 << (7 - bit%8)
		}
		last = netip.AddrFrom4(value)
	} else {
		value := first.As16()
		for bit := prefix.Bits(); bit < 128; bit++ {
			value[bit/8] |= 1 << (7 - bit%8)
		}
		last = netip.AddrFrom16(value)
	}
	return first.String() + " 至 " + last.String()
}

func parseRulePorts(raw string) (string, int, int, error) {
	parts := strings.Split(strings.TrimSpace(raw), "-")
	if len(parts) < 1 || len(parts) > 2 {
		return "", 0, 0, fmt.Errorf("请输入单端口或端口范围，例如 443 或 8000-8100")
	}
	start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || start < 1 || start > 65535 {
		return "", 0, 0, fmt.Errorf("端口必须在 1-65535 之间")
	}
	end := start
	if len(parts) == 2 {
		end, err = strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || end < start || end > 65535 {
			return "", 0, 0, fmt.Errorf("端口范围必须递增且位于 1-65535 之间")
		}
	}
	normalized := strconv.Itoa(start)
	if end != start {
		normalized += "-" + strconv.Itoa(end)
	}
	return normalized, start, end, nil
}

func normalizeAllowRule(rule allowRule) (allowRule, error) {
	if len(rule.ID) != 8 {
		return rule, fmt.Errorf("规则 ID 无效")
	}
	if _, err := hex.DecodeString(rule.ID); err != nil {
		return rule, fmt.Errorf("规则 ID 无效")
	}
	rule.ID = strings.ToLower(rule.ID)
	if protocolMask(rule.Protocol) == 0 {
		return rule, fmt.Errorf("协议必须为 TCP、UDP 或 TCP+UDP")
	}
	if rule.Family != "" && rule.Family != "ipv4" && rule.Family != "ipv6" && rule.Family != "both" {
		return rule, fmt.Errorf("IP 版本必须为 IPv4、IPv6 或 IPv4+IPv6")
	}
	switch rule.Kind {
	case "port":
		if rule.Source != "" || rule.SourceIPv6 != "" {
			return rule, fmt.Errorf("端口放行不能指定来源地址")
		}
	case "source", "source_port":
		var err error
		rule.Source, err = normalizeSource(rule.Source)
		if err != nil {
			return rule, err
		}
		is4 := sourcePrefix(rule.Source).Addr().Is4()
		if rule.Family == "both" {
			rule.SourceIPv6, err = normalizeSource(rule.SourceIPv6)
			if err != nil || !is4 || sourcePrefix(rule.SourceIPv6).Addr().Is4() {
				return rule, fmt.Errorf("IPv4+IPv6 来源规则需要分别填写有效的 IPv4 和 IPv6 来源")
			}
		} else if rule.SourceIPv6 != "" || (rule.Family == "ipv4" && !is4) || (rule.Family == "ipv6" && is4) {
			return rule, fmt.Errorf("来源地址与所选 IP 版本不一致")
		}
	default:
		return rule, fmt.Errorf("规则类型无效")
	}
	if rule.Kind == "source" {
		if rule.Ports != "" {
			return rule, fmt.Errorf("IP 放行不能指定端口")
		}
	} else {
		var err error
		rule.Ports, _, _, err = parseRulePorts(rule.Ports)
		if err != nil {
			return rule, err
		}
	}
	return rule, nil
}

func newRuleID(rules []allowRule) (string, error) {
	for {
		var value [4]byte
		if _, err := rand.Read(value[:]); err != nil {
			return "", err
		}
		id := hex.EncodeToString(value[:])
		found := false
		for _, rule := range rules {
			found = found || rule.ID == id
		}
		if !found {
			return id, nil
		}
	}
}

func protocolMask(protocol string) int {
	switch protocol {
	case "tcp":
		return 1
	case "udp":
		return 2
	case "both":
		return 3
	}
	return 0
}

func protocolLabel(protocol string) string {
	if protocol == "both" {
		return "TCP+UDP"
	}
	return strings.ToUpper(protocol)
}

// Empty family preserves existing rules: unrestricted port rules cover both
// versions, and source rules derive their version from the source address.
func (rule allowRule) familyMask() int {
	switch rule.Family {
	case "ipv4":
		return 1
	case "ipv6":
		return 2
	case "both":
		return 3
	}
	if rule.Source != "" {
		if sourcePrefix(rule.Source).Addr().Is4() {
			return 1
		}
		return 2
	}
	return 3
}

func (rule allowRule) familyLabel() string {
	return map[int]string{1: "IPv4", 2: "IPv6", 3: "IPv4+IPv6"}[rule.familyMask()]
}

func (rule allowRule) sourceForFamily(family int) string {
	if family == 2 && rule.SourceIPv6 != "" {
		return rule.SourceIPv6
	}
	return rule.Source
}

func (rule allowRule) sourceLabel() string {
	if rule.Source == "" {
		return "任意来源（" + rule.familyLabel() + "）"
	}
	if rule.SourceIPv6 != "" {
		return rule.Source + " / " + rule.SourceIPv6
	}
	return rule.Source
}

func (rule allowRule) sourceRange() string {
	if rule.Source == "" {
		return rule.sourceLabel()
	}
	if rule.SourceIPv6 != "" {
		return "IPv4：" + sourceRange(rule.Source) + "\nIPv6：" + sourceRange(rule.SourceIPv6)
	}
	return sourceRange(rule.Source)
}

func (rule allowRule) description() string {
	source, ports := rule.sourceLabel(), rule.Ports
	if ports == "" {
		ports = "全部端口"
	}
	return source + " → " + ports + " / " + protocolLabel(rule.Protocol)
}

func (rule allowRule) nftLines() []string {
	prefixes := []string{""}
	if rule.Source != "" {
		family := "ip6"
		if sourcePrefix(rule.Source).Addr().Is4() {
			family = "ip"
		}
		prefixes = []string{family + " saddr " + rule.Source + " "}
		if rule.SourceIPv6 != "" {
			prefixes = append(prefixes, "ip6 saddr "+rule.SourceIPv6+" ")
		}
	} else if rule.familyMask() != 3 {
		prefixes = []string{"meta nfproto " + strings.ToLower(rule.familyLabel()) + " "}
	}
	var result []string
	for _, prefix := range prefixes {
		for _, protocol := range []string{"tcp", "udp"} {
			if protocolMask(rule.Protocol)&protocolMask(protocol) == 0 {
				continue
			}
			match := "meta l4proto " + protocol
			if rule.Ports != "" {
				match = protocol + " dport " + rule.Ports
			}
			result = append(result, prefix+match+` counter accept comment "snail-rule:`+rule.ID+`"`)
		}
	}
	return result
}

func (rule allowRule) portBounds() (int, int) {
	if rule.Ports == "" {
		return 1, 65535
	}
	_, start, end, _ := parseRulePorts(rule.Ports)
	return start, end
}

func (rule allowRule) covers(other allowRule) bool {
	if protocolMask(rule.Protocol)&protocolMask(other.Protocol) != protocolMask(other.Protocol) || rule.familyMask()&other.familyMask() != other.familyMask() {
		return false
	}
	for _, family := range []int{1, 2} {
		if other.familyMask()&family != 0 {
			source, otherSource := rule.sourceForFamily(family), other.sourceForFamily(family)
			if source != "" {
				if otherSource == "" {
					return false
				}
				a, b := sourcePrefix(source), sourcePrefix(otherSource)
				if a.Bits() > b.Bits() || !a.Contains(b.Addr()) {
					return false
				}
			}
		}
	}
	a, b := rule.portBounds()
	c, d := other.portBounds()
	return a <= c && b >= d
}

func (rule allowRule) overlaps(other allowRule) bool {
	if protocolMask(rule.Protocol)&protocolMask(other.Protocol) == 0 {
		return false
	}
	familiesOverlap := false
	for _, family := range []int{1, 2} {
		if rule.familyMask()&other.familyMask()&family != 0 {
			source, otherSource := rule.sourceForFamily(family), other.sourceForFamily(family)
			if source == "" || otherSource == "" || sourcePrefix(source).Overlaps(sourcePrefix(otherSource)) {
				familiesOverlap = true
			}
		}
	}
	if !familiesOverlap {
		return false
	}
	a, b := rule.portBounds()
	c, d := other.portBounds()
	return a <= d && c <= b
}

func checkRuleRelations(candidate allowRule, rules []allowRule) ([]string, error) {
	var warnings []string
	for _, other := range rules {
		if other.ID == candidate.ID {
			continue
		}
		if other.Kind == candidate.Kind && other.familyMask() == candidate.familyMask() && other.Source == candidate.Source && other.SourceIPv6 == candidate.SourceIPv6 && other.Ports == candidate.Ports && other.Protocol == candidate.Protocol {
			return nil, fmt.Errorf("与规则 %s 完全重复，未保存", other.ID)
		}
		if !candidate.overlaps(other) {
			continue
		}
		relation := "部分重叠"
		if other.covers(candidate) {
			relation = "已有规则覆盖新规则"
		} else if candidate.covers(other) {
			relation = "新规则覆盖已有规则"
		}
		warnings = append(warnings, other.ID+" · "+relation+" · "+other.description())
	}
	return warnings, nil
}
