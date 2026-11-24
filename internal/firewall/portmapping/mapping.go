package portmapping

import (
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"snail_tool/internal/firewall/configuration"
)

const (
	Begin             = "# ===== BEGIN SNAIL PORT MAPPINGS ====="
	End               = "# ===== END SNAIL PORT MAPPINGS ====="
	portMappingHeader = "    chain port_mappings {\n        type nat hook prerouting priority -99; policy accept;\n"
)

const portMappingMetadata = "        # SNAIL PORT MAPPING "

const (
	portMappingSNATHeader       = "    chain port_mapping_snat {\n        type nat hook postrouting priority 99; policy accept;\n"
	legacyPortMappingSNATHeader = "    chain port_mapping_snat {\n        type nat hook postrouting priority 101; policy accept;\n"
)

type portRange struct{ first, last int }

type mappingEndpoint struct {
	addr  netip.Addr
	ports portRange
}

func (endpoint mappingEndpoint) String() string {
	host := "*"
	if endpoint.addr.IsValid() {
		host = endpoint.addr.String()
	}
	return net.JoinHostPort(host, mappingPortText(endpoint.ports))
}

type Mapping struct {
	protocol string
	from, to mappingEndpoint
	iface    string
	source   netip.Prefix
	disabled bool
	snat     portMappingSNAT
}

type portMappingConfig struct {
	Protocol string                 `json:"protocol"`
	From     string                 `json:"from"`
	To       string                 `json:"to"`
	Iface    string                 `json:"interface,omitempty"`
	Source   string                 `json:"source,omitempty"`
	Disabled bool                   `json:"disabled,omitempty"`
	SNAT     *portMappingSNATConfig `json:"snat,omitempty"`
}

func (mapping Mapping) config() portMappingConfig {
	value := portMappingConfig{
		Protocol: mapping.protocol, From: mapping.from.String(), To: mapping.to.String(),
		Iface: mapping.iface, Disabled: mapping.disabled, SNAT: mapping.snat.config(),
	}
	if mapping.source.IsValid() {
		value.Source = mapping.source.String()
	}
	return value
}

func parsePortMapping(protocol, from, to string) (Mapping, error) {
	return (portMappingConfig{Protocol: protocol, From: from, To: to}).parse()
}

func (value portMappingConfig) parse() (Mapping, error) {
	protocol := strings.ToLower(value.Protocol)
	if protocol != "tcp" && protocol != "udp" && protocol != "both" {
		return Mapping{}, fmt.Errorf("端口映射协议须为 TCP、UDP 或 both（两者）")
	}
	from, err := parseMappingEndpoint(value.From, true)
	if err != nil {
		return Mapping{}, fmt.Errorf("本机端点无效：%w", err)
	}
	to, err := parseMappingEndpoint(value.To, false)
	if err != nil {
		return Mapping{}, fmt.Errorf("目标端点无效：%w", err)
	}
	if from.addr.IsValid() && from.addr.Is4() != to.addr.Is4() {
		return Mapping{}, fmt.Errorf("本机地址与目标地址必须同为 IPv4 或同为 IPv6")
	}
	if from == to {
		return Mapping{}, fmt.Errorf("本机与目标地址端口相同，无需映射")
	}
	if to.ports.first != to.ports.last && to.ports.last-to.ports.first != from.ports.last-from.ports.first {
		return Mapping{}, fmt.Errorf("目标须为单个端口，或与本机端口范围包含相同数量的端口")
	}
	iface, err := configuration.NormalizeInterface(value.Iface)
	if err != nil {
		return Mapping{}, err
	}
	if strings.Contains(iface, "*") || iface == "." || iface == ".." {
		return Mapping{}, fmt.Errorf("请输入确切的入站网卡名，不使用通配符")
	}

	var source netip.Prefix
	if value.Source != "" {
		source, err = netip.ParsePrefix(value.Source)
		if err != nil {
			addr, addrErr := netip.ParseAddr(value.Source)
			if addrErr != nil || addr.Zone() != "" {
				return Mapping{}, fmt.Errorf("请输入有效的来源 IP 或 CIDR 网段")
			}
			source = netip.PrefixFrom(addr, addr.BitLen())
		}
		if source.Addr().Is4In6() || source.Addr().Is4() != to.addr.Is4() {
			return Mapping{}, fmt.Errorf("来源地址与目标地址必须属于同一协议族")
		}
		source = source.Masked()
	}
	snat, err := value.SNAT.parse(to.addr)
	if err != nil {
		return Mapping{}, err
	}
	return Mapping{protocol: protocol, from: from, to: to, iface: iface, source: source, disabled: value.Disabled, snat: snat}, nil
}

func parseMappingEndpoint(raw string, allowAny bool) (mappingEndpoint, error) {
	host, port, err := net.SplitHostPort(raw)
	if err != nil {
		return mappingEndpoint{}, fmt.Errorf("请输入 IP:端口，IPv6 使用 [地址]:端口")
	}
	var addr netip.Addr
	if host != "*" || !allowAny {
		addr, err = netip.ParseAddr(host)
		if err != nil || !validMappingAddress(addr) {
			return mappingEndpoint{}, fmt.Errorf("请输入有效的单播 IP")
		}
	}
	_, first, last, err := configuration.ParsePortRange(port)
	if err != nil {
		return mappingEndpoint{}, err
	}
	return mappingEndpoint{addr: addr, ports: portRange{first, last}}, nil
}

func validMappingAddress(addr netip.Addr) bool {
	return !addr.Is4In6() && addr.Zone() == "" && (addr.IsGlobalUnicast() || addr.IsLoopback() || addr.IsLinkLocalUnicast())
}

func mappingPortText(ports portRange) string {
	if ports.first == ports.last {
		return strconv.Itoa(ports.first)
	}
	return fmt.Sprintf("%d-%d", ports.first, ports.last)
}

func (mapping Mapping) familyLabel() string {
	if mapping.to.addr.Is6() {
		return "IPv6"
	}
	return "IPv4"
}

func (mapping Mapping) endpointsLabel() string {
	return mapping.from.String() + " → " + mapping.to.String()
}

func (mapping Mapping) matchLabels() []string {
	var fields []string
	if mapping.iface != "" {
		fields = append(fields, "入站 "+mapping.iface)
	}
	if mapping.source.IsValid() {
		fields = append(fields, "来源 "+mapping.source.String())
	}
	return fields
}

func (mapping Mapping) description() string {
	fields := []string{mapping.familyLabel() + " " + configuration.ProtocolLabel(mapping.protocol) + " " + mapping.endpointsLabel()}
	if mapping.disabled {
		fields = append(fields, "已停用")
	}
	fields = append(fields, mapping.matchLabels()...)
	if mapping.snat.address.IsValid() {
		fields = append(fields, "SNAT "+mapping.snatDescription())
	}
	return strings.Join(fields, " · ")
}

func (mapping Mapping) overlaps(other Mapping) bool {
	return !mapping.disabled && !other.disabled && mapping.to.addr.Is4() == other.to.addr.Is4() &&
		(mapping.protocol == other.protocol || mapping.protocol == "both" || other.protocol == "both") &&
		(mapping.iface == "" || other.iface == "" || mapping.iface == other.iface) &&
		(!mapping.source.IsValid() || !other.source.IsValid() || mapping.source.Overlaps(other.source)) &&
		(!mapping.from.addr.IsValid() || !other.from.addr.IsValid() || mapping.from.addr == other.from.addr) &&
		mapping.from.ports.first <= other.from.ports.last && other.from.ports.first <= mapping.from.ports.last
}

func (mapping Mapping) protocols() []string {
	if mapping.protocol == "both" {
		return []string{"tcp", "udp"}
	}
	return []string{mapping.protocol}
}

func (mapping Mapping) writeRules(block *strings.Builder) {
	if mapping.disabled {
		return
	}
	family, nfproto := "ip", "ipv4"
	if mapping.to.addr.Is6() {
		family, nfproto = "ip6", "ipv6"
	}
	for _, protocol := range mapping.protocols() {
		block.WriteString("        ")
		if mapping.iface != "" {
			fmt.Fprintf(block, "iifname %q ", mapping.iface)
		}
		block.WriteString("fib daddr type local ")
		if mapping.from.addr.IsValid() {
			fmt.Fprintf(block, "%s daddr %s ", family, mapping.from.addr)
		} else {
			fmt.Fprintf(block, "meta nfproto %s ", nfproto)
		}
		if mapping.source.IsValid() {
			fmt.Fprintf(block, "%s saddr %s ", family, mapping.source)
		}
		fmt.Fprintf(block, "%s dport %s dnat %s to ", protocol, mappingPortText(mapping.from.ports), family)
		switch {
		case mapping.to.ports.first == mapping.to.ports.last:
			fmt.Fprintln(block, netip.AddrPortFrom(mapping.to.addr, uint16(mapping.to.ports.first)))
		case mapping.from.ports == mapping.to.ports:
			fmt.Fprintln(block, mapping.to.addr)
		default:
			address := mapping.to.addr.String()
			if mapping.to.addr.Is6() {
				address = "[" + address + "]"
			}
			fmt.Fprintf(block, "%s : %s dport map { ", address, protocol)
			for port := mapping.from.ports.first; port <= mapping.from.ports.last; port++ {
				if port > mapping.from.ports.first {
					block.WriteString(", ")
				}
				fmt.Fprintf(block, "%d : %d", port, mapping.to.ports.first+port-mapping.from.ports.first)
			}
			block.WriteString(" }\n")
		}
	}
}

func portMappingBlock(mappings []Mapping, metadata bool) string {
	var block, snatRules strings.Builder
	block.WriteString(Begin + "\n" + portMappingHeader)
	for _, mapping := range mappings {
		if metadata {
			value, _ := json.Marshal(mapping.config())
			block.WriteString(portMappingMetadata + string(value) + "\n")
		}
		mapping.writeRules(&block)
		mapping.writeSNATRules(&snatRules)
	}
	block.WriteString("    }\n")
	if snatRules.Len() > 0 {
		block.WriteString(portMappingSNATHeader)
		block.WriteString(snatRules.String())
		block.WriteString("    }\n")
	}
	block.WriteString(End + "\n")
	return block.String()
}

func portMappingRegion(content string) (int, int, error) {
	tokens, err := configuration.Tokens(content)
	if err != nil {
		return 0, 0, err
	}
	open, close := -1, -1
	for i := 0; i+3 < len(tokens); i++ {
		if tokens[i].Text != "table" || tokens[i+1].Text != "inet" || tokens[i+2].Text != "snail" || tokens[i+3].Text != "{" {
			continue
		}
		if open >= 0 {
			return 0, 0, fmt.Errorf("配置中存在多个 inet snail 表")
		}
		end, err := configuration.ClosingBrace(tokens, i+3)
		if err != nil {
			return 0, 0, err
		}
		open, close = tokens[i+3].End, tokens[end].Start
		i = end
	}
	if open < 0 {
		return 0, 0, fmt.Errorf("未找到 inet snail 表，请先生成基础配置")
	}

	begins, ends := strings.Count(content, Begin), strings.Count(content, End)
	if begins == 0 && ends == 0 {
		return close, close, nil
	}
	begin := strings.Index(content, Begin)
	end := strings.Index(content, End) + len(End)
	if begins != 1 || ends != 1 || begin <= open || end <= begin || end >= close || content[end] != '\n' {
		return 0, 0, fmt.Errorf("端口映射托管标记异常")
	}
	return begin, end + 1, nil
}

func Read(content string) ([]Mapping, error) {
	if !strings.Contains(content, Begin) && !strings.Contains(content, End) {
		return nil, nil
	}
	begin, end, err := portMappingRegion(content)
	if err != nil {
		return nil, err
	}
	var mappings []Mapping
	block := content[begin:end]
	afterHeader, found := strings.CutPrefix(block, Begin+"\n"+portMappingHeader)
	ruleText, closed := strings.CutSuffix(afterHeader, "    }\n"+End+"\n")
	if !found || !closed {
		return nil, fmt.Errorf("端口映射托管区块内容已修改")
	}
	metadata := strings.Contains(ruleText, portMappingMetadata)
	for _, line := range strings.Split(strings.TrimSuffix(ruleText, "\n"), "\n") {
		switch {
		case line == "":
			continue
		case metadata:
			payload, found := strings.CutPrefix(line, portMappingMetadata)
			if !found {
				continue
			}
			var config portMappingConfig
			if err := json.Unmarshal([]byte(payload), &config); err != nil {
				return nil, fmt.Errorf("端口映射配置无法解析：%w", err)
			}
			mapping, err := config.parse()
			if err != nil {
				return nil, err
			}
			mappings = append(mappings, mapping)
		default:
			mapping, err := readLegacyPortMapping(line)
			if err != nil {
				return nil, err
			}
			mappings = append(mappings, mapping)
		}
	}
	expected := portMappingBlock(mappings, metadata)
	if block != expected && block != strings.Replace(expected, portMappingSNATHeader, legacyPortMappingSNATHeader, 1) {
		return nil, fmt.Errorf("端口映射托管区块内容已修改")
	}
	return mappings, nil
}

func readLegacyPortMapping(line string) (Mapping, error) {
	fields := strings.Fields(line)
	if len(fields) != 14 {
		return Mapping{}, fmt.Errorf("端口映射规则格式无法识别")
	}
	return parsePortMapping(fields[7], net.JoinHostPort(fields[6], fields[9]), fields[13])
}

func Replace(content string, mappings []Mapping) (string, error) {
	if err := validatePortMappingSNAT(mappings); err != nil {
		return "", err
	}
	if len(mappings) == 0 && !strings.Contains(content, Begin) {
		return content, nil
	}
	begin, end, err := portMappingRegion(content)
	if err != nil {
		return "", err
	}
	if len(mappings) == 0 {
		return content[:begin] + content[end:], nil
	}
	prefix := content[:begin]
	if !strings.HasSuffix(prefix, "\n") {
		prefix += "\n"
	}
	return prefix + portMappingBlock(mappings, true) + content[end:], nil
}
