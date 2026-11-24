package forward

import (
	"snail_tool/internal/firewall/forward/dockerbridge"
	"snail_tool/internal/firewall/forward/dockermark"
	"snail_tool/internal/firewall/internal/rules"
	"snail_tool/internal/ui"
)

var Lists = []rules.PolicyList{
	{Name: "snail_forward_public_ipv4_tcp_ports", Label: "公网 IPv4 TCP", NamedSet: true},
	{Name: "snail_forward_public_ipv4_udp_ports", Label: "公网 IPv4 UDP", NamedSet: true},
	{Name: "snail_forward_public_ipv6_tcp_ports", Label: "公网 IPv6 TCP", NamedSet: true},
	{Name: "snail_forward_public_ipv6_udp_ports", Label: "公网 IPv6 UDP", NamedSet: true},
	{Name: "snail_forward_private_ipv4_tcp_ports", Label: "内网 IPv4 TCP", NamedSet: true},
	{Name: "snail_forward_private_ipv4_udp_ports", Label: "内网 IPv4 UDP", NamedSet: true},
	{Name: "snail_forward_private_ipv6_tcp_ports", Label: "内网 IPv6 TCP", NamedSet: true},
	{Name: "snail_forward_private_ipv6_udp_ports", Label: "内网 IPv6 UDP", NamedSet: true},
	{Name: "snail_forward_private_ipv4", Label: "IPv4 内网范围", Family: 4, Defaults: "172.16.0.0/12, 192.168.0.0/16"},
	{Name: "snail_forward_private_ipv6", Label: "IPv6 内网范围", Family: 6, Defaults: "fc00::/7, fe80::/10"},
}

func SummaryFields(content string) []ui.CardField {
	managed, err := rules.ManagedRulesIn(content, rules.ForwardBlock)
	return rules.Summarize(content, Lists).Fields(len(managed), err)
}

func Service(options rules.Options, bridge dockerbridge.Service, mark dockermark.Service) rules.Session {
	actions := rules.ImmediateActions{
		Fields: func(content string) []ui.CardField { return append(bridge.StateFields(content), mark.StateField()) },
		Print:  printDockerOptions,
		Choose: func(view *ui.UI, choice string) (func() error, string) {
			return dockerChoice(choice, func() error { return bridge.Run(view) }, func() error { return mark.Run(view) })
		},
	}
	return rules.NewForward(options, Title, Label, Lists, SummaryFields, actions)
}
