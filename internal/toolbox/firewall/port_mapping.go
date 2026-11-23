package firewall

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

const (
	portMappingsBegin = "# ===== BEGIN SNAIL PORT MAPPINGS ====="
	portMappingsEnd   = "# ===== END SNAIL PORT MAPPINGS ====="
	portMappingHeader = "    chain port_mappings {\n        type nat hook prerouting priority -99; policy accept;\n"
)

const portMappingMetadata = "        # SNAIL PORT MAPPING "

type mappingEndpoint struct {
	addr  netip.Addr
	ports portInterval
}

func (endpoint mappingEndpoint) String() string {
	host := "*"
	if endpoint.addr.IsValid() {
		host = endpoint.addr.String()
	}
	return net.JoinHostPort(host, mappingPortText(endpoint.ports))
}

type portMapping struct {
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

func (mapping portMapping) config() portMappingConfig {
	value := portMappingConfig{
		Protocol: mapping.protocol, From: mapping.from.String(), To: mapping.to.String(),
		Iface: mapping.iface, Disabled: mapping.disabled, SNAT: mapping.snat.config(),
	}
	if mapping.source.IsValid() {
		value.Source = mapping.source.String()
	}
	return value
}

func parsePortMapping(protocol, from, to string) (portMapping, error) {
	return (portMappingConfig{Protocol: protocol, From: from, To: to}).parse()
}

func (value portMappingConfig) parse() (portMapping, error) {
	protocol := strings.ToLower(value.Protocol)
	if protocol != "tcp" && protocol != "udp" && protocol != "both" {
		return portMapping{}, fmt.Errorf("端口映射协议须为 TCP、UDP 或 both（两者）")
	}
	from, err := parseMappingEndpoint(value.From, true)
	if err != nil {
		return portMapping{}, fmt.Errorf("本机端点无效：%w", err)
	}
	to, err := parseMappingEndpoint(value.To, false)
	if err != nil {
		return portMapping{}, fmt.Errorf("目标端点无效：%w", err)
	}
	if from.addr.IsValid() && from.addr.Is4() != to.addr.Is4() {
		return portMapping{}, fmt.Errorf("本机地址与目标地址必须同为 IPv4 或同为 IPv6")
	}
	if from == to {
		return portMapping{}, fmt.Errorf("本机与目标地址端口相同，无需映射")
	}
	if to.ports.first != to.ports.last && to.ports.last-to.ports.first != from.ports.last-from.ports.first {
		return portMapping{}, fmt.Errorf("目标须为单个端口，或与本机端口范围包含相同数量的端口")
	}
	iface, err := normalizeIface(value.Iface)
	if err != nil {
		return portMapping{}, err
	}
	if strings.Contains(iface, "*") || iface == "." || iface == ".." {
		return portMapping{}, fmt.Errorf("请输入确切的入站网卡名，不使用通配符")
	}

	var source netip.Prefix
	if value.Source != "" {
		source, err = netip.ParsePrefix(value.Source)
		if err != nil {
			addr, addrErr := netip.ParseAddr(value.Source)
			if addrErr != nil || addr.Zone() != "" {
				return portMapping{}, fmt.Errorf("请输入有效的来源 IP 或 CIDR 网段")
			}
			source = netip.PrefixFrom(addr, addr.BitLen())
		}
		if source.Addr().Is4In6() || source.Addr().Is4() != to.addr.Is4() {
			return portMapping{}, fmt.Errorf("来源地址与目标地址必须属于同一协议族")
		}
		source = source.Masked()
	}
	snat, err := value.SNAT.parse(to.addr)
	if err != nil {
		return portMapping{}, err
	}
	return portMapping{protocol: protocol, from: from, to: to, iface: iface, source: source, disabled: value.Disabled, snat: snat}, nil
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
	_, first, last, err := parseRulePorts(port)
	if err != nil {
		return mappingEndpoint{}, err
	}
	return mappingEndpoint{addr: addr, ports: portInterval{first, last}}, nil
}

func validMappingAddress(addr netip.Addr) bool {
	return !addr.Is4In6() && addr.Zone() == "" && (addr.IsGlobalUnicast() || addr.IsLoopback() || addr.IsLinkLocalUnicast())
}

func mappingPortText(ports portInterval) string {
	if ports.first == ports.last {
		return strconv.Itoa(ports.first)
	}
	return fmt.Sprintf("%d-%d", ports.first, ports.last)
}

func (mapping portMapping) description() string {
	protocol := strings.ToUpper(mapping.protocol)
	if mapping.protocol == "both" {
		protocol = "TCP/UDP"
	}
	version := "IPv4"
	if mapping.to.addr.Is6() {
		version = "IPv6"
	}
	fields := []string{version + " " + protocol + " " + mapping.from.String() + " → " + mapping.to.String()}
	if mapping.iface != "" {
		fields = append(fields, "入站 "+mapping.iface)
	}
	if mapping.source.IsValid() {
		fields = append(fields, "来源 "+mapping.source.String())
	}
	if mapping.disabled {
		fields = append(fields, "已停用")
	}
	if mapping.snat.address.IsValid() {
		fields = append(fields, "SNAT "+mapping.snatDescription())
	}
	return strings.Join(fields, " · ")
}

func (mapping portMapping) overlaps(other portMapping) bool {
	return !mapping.disabled && !other.disabled && mapping.to.addr.Is4() == other.to.addr.Is4() &&
		(mapping.protocol == other.protocol || mapping.protocol == "both" || other.protocol == "both") &&
		(mapping.iface == "" || other.iface == "" || mapping.iface == other.iface) &&
		(!mapping.source.IsValid() || !other.source.IsValid() || mapping.source.Overlaps(other.source)) &&
		(!mapping.from.addr.IsValid() || !other.from.addr.IsValid() || mapping.from.addr == other.from.addr) &&
		mapping.from.ports.first <= other.from.ports.last && other.from.ports.first <= mapping.from.ports.last
}

func (mapping portMapping) protocols() []string {
	if mapping.protocol == "both" {
		return []string{"tcp", "udp"}
	}
	return []string{mapping.protocol}
}

func (mapping portMapping) writeRules(block *strings.Builder) {
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

func portMappingBlock(mappings []portMapping, metadata bool) string {
	var block, snatRules strings.Builder
	block.WriteString(portMappingsBegin + "\n" + portMappingHeader)
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
		block.WriteString("    chain port_mapping_snat {\n        type nat hook postrouting priority 101; policy accept;\n")
		block.WriteString(snatRules.String())
		block.WriteString("    }\n")
	}
	block.WriteString(portMappingsEnd + "\n")
	return block.String()
}

func portMappingRegion(content string) (int, int, error) {
	tokens, err := configTokens(content)
	if err != nil {
		return 0, 0, err
	}
	open, close := -1, -1
	for i := 0; i+3 < len(tokens); i++ {
		if tokens[i].text != "table" || tokens[i+1].text != "inet" || tokens[i+2].text != "snail" || tokens[i+3].text != "{" {
			continue
		}
		if open >= 0 {
			return 0, 0, fmt.Errorf("配置中存在多个 inet snail 表")
		}
		end, err := closingBrace(tokens, i+3)
		if err != nil {
			return 0, 0, err
		}
		open, close = tokens[i+3].end, tokens[end].start
		i = end
	}
	if open < 0 {
		return 0, 0, fmt.Errorf("未找到 inet snail 表，请先生成基础配置")
	}

	begins, ends := strings.Count(content, portMappingsBegin), strings.Count(content, portMappingsEnd)
	if begins == 0 && ends == 0 {
		return close, close, nil
	}
	begin := strings.Index(content, portMappingsBegin)
	end := strings.Index(content, portMappingsEnd) + len(portMappingsEnd)
	if begins != 1 || ends != 1 || begin <= open || end <= begin || end >= close || content[end] != '\n' {
		return 0, 0, fmt.Errorf("端口映射托管标记异常")
	}
	return begin, end + 1, nil
}

func readPortMappings(content string) ([]portMapping, error) {
	if !strings.Contains(content, portMappingsBegin) && !strings.Contains(content, portMappingsEnd) {
		return nil, nil
	}
	begin, end, err := portMappingRegion(content)
	if err != nil {
		return nil, err
	}
	var mappings []portMapping
	block := content[begin:end]
	afterHeader, found := strings.CutPrefix(block, portMappingsBegin+"\n"+portMappingHeader)
	ruleText, closed := strings.CutSuffix(afterHeader, "    }\n"+portMappingsEnd+"\n")
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
	if portMappingBlock(mappings, metadata) != block {
		return nil, fmt.Errorf("端口映射托管区块内容已修改")
	}
	return mappings, nil
}

func readLegacyPortMapping(line string) (portMapping, error) {
	fields := strings.Fields(line)
	if len(fields) != 14 {
		return portMapping{}, fmt.Errorf("端口映射规则格式无法识别")
	}
	return parsePortMapping(fields[7], net.JoinHostPort(fields[6], fields[9]), fields[13])
}

func withPortMappings(content string, mappings []portMapping) (string, error) {
	if err := validatePortMappingSNAT(mappings); err != nil {
		return "", err
	}
	if len(mappings) == 0 && !strings.Contains(content, portMappingsBegin) {
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

func runPortMappingManager(view *ui.UI) error {
	for {
		version, err := readConfigVersion()
		if err != nil {
			return err
		}
		content := string(version.content)
		if !hasManagedConfig(content) {
			return fmt.Errorf("请先选择 d 生成本工具防火墙配置")
		}
		mappings, err := readPortMappings(content)
		if err != nil {
			return err
		}
		ui.ClearScreen()
		ui.MenuTitle("防火墙管理", "端口映射管理")
		if len(mappings) == 0 {
			ui.PrintInfoCard("尚未配置端口映射")
		} else {
			fields := make([]ui.CardField, 0, len(mappings))
			for i, mapping := range mappings {
				fields = append(fields, ui.CardField{Label: strconv.Itoa(i + 1), Value: mapping.description()})
			}
			ui.PrintInfoCard("已保存的端口映射", fields...)
		}
		ui.MenuOption("1", "添加端口映射")
		options := map[string]string{"1": "添加"}
		if len(mappings) > 0 {
			ui.MenuOption("2", "删除端口映射")
			ui.MenuOption("3", "编辑端口映射")
			ui.MenuOption("4", "启用或停用端口映射")
			ui.MenuOption("5", "SNAT 设置")
			options["2"], options["3"], options["4"], options["5"] = "删除", "编辑", "切换", "SNAT"
		}
		ui.MenuExit("0/q", "返回防火墙管理")
		action, err := askRuleChoice(view, "请选择", "", options)
		if err != nil {
			return err
		}
		err = changePortMapping(view, version, mappings, action)
		if errors.Is(err, shared.ErrReturnToMenu) {
			continue
		}
		return err
	}
}

func changePortMapping(view *ui.UI, version configVersion, mappings []portMapping, action string) error {
	index := len(mappings)
	var current portMapping
	if action != "添加" {
		line, err := view.Ask("输入映射编号（0/q 返回端口映射列表）：")
		if err != nil {
			return err
		}
		if shared.IsReturnChoice(line) {
			return shared.ErrReturnToMenu
		}
		number, err := strconv.Atoi(line)
		if err != nil || number < 1 || number > len(mappings) {
			return fmt.Errorf("端口映射编号无效")
		}
		index = number - 1
		current = mappings[index]
	}

	var mapping portMapping
	switch action {
	case "添加", "编辑":
		if action == "编辑" {
			ui.PrintInfoCard("正在编辑", ui.CardField{Label: "当前配置", Value: current.description()})
		}
		updated, err := askPortMapping(view, current)
		if err != nil {
			return err
		}
		mapping = updated
		if action == "编辑" && mapping == current {
			return shared.ErrReturnToMenu
		}
	case "SNAT":
		updated, change, err := askPortMappingSNAT(view, current)
		if err != nil {
			return err
		}
		if updated == current.snat {
			return shared.ErrReturnToMenu
		}
		mapping = current
		mapping.snat, action = updated, change
	case "切换":
		mapping = current
		mapping.disabled = !mapping.disabled
		action = "停用"
		if !mapping.disabled {
			action = "启用"
		}
	case "删除":
		mapping = current
	}
	if action == "删除" {
		mappings = slices.Delete(mappings, index, index+1)
	} else {
		for i, existing := range mappings {
			if i != index && mapping.overlaps(existing) {
				return fmt.Errorf("与第 %d 条映射的匹配条件重叠：%s", i+1, existing.description())
			}
		}
		if index == len(mappings) {
			mappings = append(mappings, mapping)
		} else {
			mappings[index] = mapping
		}
	}

	updated, err := withPortMappings(string(version.content), mappings)
	if err != nil {
		return err
	}

	ui.PrintInfoCard("确认端口映射变更",
		ui.CardField{Label: "操作", Value: action + " " + mapping.description()},
		ui.CardField{Label: "转发条件", Value: "远程目标需要开启内核转发、放行转发后的目标端口，回包须经本机"},
		ui.CardField{Label: "生效方式", Value: "保存并加载整份防火墙配置，映射用于新连接"},
	)
	confirmed, err := view.Confirm("确认保存并加载？(y/N，0/q 返回)：")
	if err != nil {
		return err
	}
	if !confirmed {
		return shared.ErrReturnToMenu
	}
	if err := writeConfig(updated, version); err != nil {
		return err
	}
	if err := applyConfig([]byte(updated)); err != nil {
		return fmt.Errorf("端口映射已保存，但未成功加载，可输入 r 重试: %w", err)
	}
	ui.PrintSuccessCard("端口映射已保存并加载")
	return nil
}

func askPortMapping(view *ui.UI, current portMapping) (portMapping, error) {
	ui.PrintSectionCard("输入格式",
		ui.CardField{Label: "顺序", Value: "协议 本机IP:端口 目标IP:端口 [入站网卡] [来源IP/CIDR]", Detail: "用空格分开，方括号内的两个字段可省略"},
		ui.CardField{Label: "协议", Value: "tcp、udp、both（TCP 和 UDP）"},
		ui.CardField{Label: "地址", Value: "入口用 * 匹配同一地址族的所有本机地址。目标填实际接收地址", Detail: "两端地址族须一致，IPv6 用 [地址]:端口，例如 [fd00::20]:80"},
		ui.CardField{Label: "端口", Value: "支持单端口或连续范围。目标可填单端口或等长范围"},
		ui.CardField{Label: "匹配条件", Value: "网卡和来源同时填写时，两者都必须匹配", Detail: "只限制来源时，用 - 占住入站网卡的位置"},
		ui.CardField{Label: "出站网卡", Value: "由系统路由选择，可在「4 转发规则管理 → 5 规则配置」限制放行"},
	)
	ui.PrintSectionCard("配置示例",
		ui.CardField{Label: "不限网卡和来源", Value: "tcp *:80 192.168.1.20:80"},
		ui.CardField{Label: "只限入站网卡", Value: "tcp *:80 192.168.1.20:80 eth0"},
		ui.CardField{Label: "只限来源网段", Value: "tcp *:80 192.168.1.20:80 - 192.168.1.0/24"},
		ui.CardField{Label: "同时限制两项", Value: "tcp *:80 192.168.1.20:80 eth0 192.168.1.0/24"},
	)
	line, err := view.Ask("请输入端口映射（0/q 返回）：")
	if err != nil {
		return portMapping{}, err
	}
	if shared.IsReturnChoice(line) {
		return portMapping{}, shared.ErrReturnToMenu
	}
	fields := strings.Fields(line)
	if len(fields) < 3 || len(fields) > 5 {
		return portMapping{}, fmt.Errorf("请输入协议、本机端点、目标端点和可选的入站网卡、来源地址")
	}
	config := portMappingConfig{Protocol: fields[0], From: fields[1], To: fields[2], Disabled: current.disabled, SNAT: current.snat.config()}
	if len(fields) >= 4 && fields[3] != "-" {
		config.Iface = fields[3]
	}
	if len(fields) == 5 && fields[4] != "-" {
		config.Source = fields[4]
	}
	return config.parse()
}
