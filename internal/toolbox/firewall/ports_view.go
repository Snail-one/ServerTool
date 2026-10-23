package firewall

import (
	"fmt"
	"strings"

	"snail_tool/internal/ui"
)

func showLoadedPorts() error {
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "端口策略")
	output, err := commandOutput("nft", "-j", "list", "ruleset")
	if err != nil {
		return fmt.Errorf("读取当前端口规则失败: %w: %s", err, strings.TrimSpace(output))
	}
	summary, err := summarizeLoadedPorts(output)
	if err != nil {
		return fmt.Errorf("解析当前端口规则失败: %w", err)
	}
	defer showDockerPorts()
	if !summary.found {
		ui.PrintSectionCard("未加载本工具防火墙规则", ui.CardField{Label: "本工具表", Value: "未找到 inet snail；不能据此判断服务器端口全部开放或关闭"})
		return nil
	}
	policy := "无法识别"
	if summary.policy == "drop" {
		policy = ui.PrimaryText("默认丢弃（drop）")
	}
	if summary.policy == "accept" {
		policy = "默认放行（accept）"
	}
	fields := []ui.CardField{
		{Label: "数据来源", Value: "当前内核已加载规则"},
		{Label: "汇总范围", Value: "inet snail 的 input 分流规则；新入站 TCP / UDP 业务流量"},
		{Label: "默认策略", Value: policy},
	}
	if len(summary.otherInput) > 0 {
		fields = append(fields, ui.CardField{Label: "其他入站链", Value: strings.Join(summary.otherInput, "；"), Detail: "这些链仍可能进一步限制访问"})
	}
	ui.PrintSectionCard("已加载端口策略", fields...)
	if summary.problem != "" {
		ui.PrintSectionCard("无法完整汇总端口",
			ui.CardField{Label: "原因", Value: summary.problem},
			ui.CardField{Label: "查看详情", Value: "返回后选择 2 查看全部已加载规则"},
		)
		return nil
	}
	var ports []ui.CardField
	for scope, label := range []string{"公网", "内网"} {
		for family, version := range []string{"IPv4", "IPv6"} {
			for protocol, name := range []string{"TCP", "UDP"} {
				value := summary.ports[scope][family][protocol].String()
				if scope == 1 && len(summary.private[family]) == 0 {
					value = "没有匹配的内网来源"
				}
				ports = append(ports, ui.CardField{Label: label + " " + version + " " + name, Value: value})
			}
		}
	}
	ui.PrintSectionCard("普通来源放行端口", ports...)
	ui.PrintSectionCard("来源划分",
		ui.CardField{Label: "内网 IPv4", Value: strings.Join(summary.private[0], ", ")},
		ui.CardField{Label: "内网 IPv6", Value: strings.Join(summary.private[1], ", ")},
		ui.CardField{Label: "公网", Value: "上述内网来源之外的地址"},
	)
	var exceptions, system []ui.CardField
	for _, rule := range mergePortExceptions(summary.exceptions) {
		if rule.families == 2 && rule.protocols == 2 && rule.sources == nil && rule.hasPorts && rule.hasSourcePorts && rule.ports.String() == "546" && rule.sourcePorts.String() == "547" {
			system = append(system, ui.CardField{Label: "DHCPv6 客户端", Value: rule.description()})
		} else {
			exceptions = append(exceptions, ui.CardField{Label: "放行", Value: rule.description()})
		}
	}
	if len(exceptions) > 0 {
		ui.PrintSectionCard("附加放行（来源规则可绕过普通端口列表）", exceptions...)
	}
	if len(summary.system) > 0 {
		system = append(system, ui.CardField{Label: "其他系统流量", Value: strings.Join(summary.system, "、"), Detail: "具体条件见完整已加载规则"})
	}
	if len(system) > 0 {
		ui.PrintSectionCard("系统流量例外", system...)
	}
	fmt.Println("以上是本工具入站规则的放行范围；服务监听、其他规则及云安全组也会影响实际连通性。")
	return nil
}
