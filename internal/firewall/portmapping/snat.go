package portmapping

import (
	"fmt"
	"net/netip"
	"strings"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/ui"
)

type portMappingSNAT struct {
	iface    string
	address  netip.Addr
	disabled bool
}

type portMappingSNATConfig struct {
	Iface    string `json:"interface"`
	Address  string `json:"address"`
	Disabled bool   `json:"disabled,omitempty"`
}

func (snat portMappingSNAT) config() *portMappingSNATConfig {
	if !snat.address.IsValid() {
		return nil
	}
	return &portMappingSNATConfig{Iface: snat.iface, Address: snat.address.String(), Disabled: snat.disabled}
}

func (config *portMappingSNATConfig) parse(target netip.Addr) (portMappingSNAT, error) {
	if config == nil {
		return portMappingSNAT{}, nil
	}
	iface, err := configuration.NormalizeInterface(config.Iface)
	if err != nil {
		return portMappingSNAT{}, err
	}
	if iface == "" || strings.Contains(iface, "*") || iface == "." || iface == ".." {
		return portMappingSNAT{}, fmt.Errorf("SNAT 需要指定确切的出站网卡名")
	}
	address, err := netip.ParseAddr(config.Address)
	if err != nil || !validMappingAddress(address) || address.Is4() != target.Is4() {
		return portMappingSNAT{}, fmt.Errorf("SNAT 源 IP 必须是与映射目标同地址族的有效单播 IP")
	}
	return portMappingSNAT{iface: iface, address: address, disabled: config.Disabled}, nil
}

func (mapping Mapping) snatEnabled() bool {
	return !mapping.disabled && mapping.snat.address.IsValid() && !mapping.snat.disabled
}

func (mapping Mapping) snatDescription() string {
	switch {
	case !mapping.snat.address.IsValid():
		return "未配置"
	case mapping.snat.disabled:
		return "已停用"
	case mapping.disabled:
		return "随端口映射暂停"
	default:
		return fmt.Sprintf("已开启（%s，源 IP %s）", mapping.snat.iface, mapping.snat.address)
	}
}

// The postrouting hook sees the translated target and conntrack's original
// tuple. It cannot distinguish two otherwise identical mappings by ingress.
func validatePortMappingSNAT(mappings []Mapping) error {
	for i, mapping := range mappings {
		if !mapping.snatEnabled() {
			continue
		}
		for j, other := range mappings {
			if i == j || mapping.to.addr != other.to.addr ||
				mapping.to.ports.first > other.to.ports.last || other.to.ports.first > mapping.to.ports.last {
				continue
			}
			candidate := mapping
			candidate.iface, other.iface = "", ""
			if candidate.overlaps(other) {
				return fmt.Errorf("第 %d 条映射的 SNAT 与第 %d 条映射无法区分，请调整入口端口、来源或目标", i+1, j+1)
			}
		}
	}
	return nil
}

func (mapping Mapping) writeSNATRules(block *strings.Builder) {
	if !mapping.snatEnabled() {
		return
	}
	family := "ip"
	if mapping.to.addr.Is6() {
		family = "ip6"
	}
	for _, protocol := range mapping.protocols() {
		fmt.Fprintf(block, "        oifname %q ct direction original ct status dnat %s daddr %s %s dport %s ",
			mapping.snat.iface, family, mapping.to.addr, protocol, mappingPortText(mapping.to.ports))
		if mapping.from.addr.IsValid() {
			fmt.Fprintf(block, "ct original %s daddr %s ", family, mapping.from.addr)
		}
		if mapping.source.IsValid() {
			fmt.Fprintf(block, "ct original %s saddr %s ", family, mapping.source)
		}
		fmt.Fprintf(block, "ct original proto-dst %s snat %s to %s\n", mappingPortText(mapping.from.ports), family, mapping.snat.address)
	}
}

func askPortMappingSNAT(view *ui.UI, mapping Mapping) (portMappingSNAT, string, error) {
	current := mapping.snat
	ui.PrintInfoCard("SNAT 设置",
		ui.CardField{Label: "端口映射", Value: mapping.description()},
		ui.CardField{Label: "当前状态", Value: mapping.snatDescription()},
	)
	if current.address.IsValid() {
		toggle := "停用"
		if current.disabled {
			toggle = "启用"
		}
		ui.MenuOption("1", toggle+" SNAT")
		ui.MenuOption("2", "修改 SNAT 参数")
		ui.MenuExit("0/q", "返回映射详情")
		action, err := ui.AskChoice(view, "请选择", "", map[string]string{"1": "toggle", "2": "edit"})
		if err != nil {
			return current, "", err
		}
		if action == "toggle" {
			current.disabled = !current.disabled
			return current, toggle + " SNAT", nil
		}
	}

	example := "wg0 10.77.0.1"
	if mapping.to.addr.Is6() {
		example = "wg0 fd00::1"
	}
	ui.PrintField("输入格式", "出站网卡 转换后的源IP，示例 "+example)
	if current.address.IsValid() {
		ui.PrintField("现有参数", current.iface+" "+current.address.String())
	}
	line, err := view.Ask("请输入 SNAT 参数（0/q 返回）：")
	if err != nil {
		return current, "", err
	}
	if ui.IsReturnChoice(line) {
		return current, "", ui.ErrReturnToMenu
	}
	fields := strings.Fields(line)
	if len(fields) != 2 {
		return current, "", fmt.Errorf("请输入出站网卡和转换后的源 IP")
	}
	config := portMappingSNATConfig{Iface: fields[0], Address: fields[1], Disabled: current.disabled}
	updated, err := config.parse(mapping.to.addr)
	if err != nil {
		return current, "", err
	}
	action := "配置并启用 SNAT"
	if current.address.IsValid() {
		action = "修改 SNAT 参数"
	}
	return updated, action, nil
}
