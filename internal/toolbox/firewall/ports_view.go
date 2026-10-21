package firewall

import (
	"fmt"
	"strings"

	"snail_tool/internal/ui"
)

func showLoadedPorts() error {
	output, err := commandOutput("nft", "-j", "list", "ruleset")
	if err != nil {
		return fmt.Errorf("读取当前端口规则失败: %w: %s", err, strings.TrimSpace(output))
	}
	summary, err := summarizeLoadedPorts(output)
	if err != nil {
		return fmt.Errorf("解析当前端口规则失败: %w", err)
	}
	ui.MenuTitle("防火墙管理", "当前生效端口")
	if !summary.found {
		ui.PrintInfoCard("未加载本工具防火墙规则", ui.CardField{Label: "本工具表", Value: "未找到 inet snail；不能据此判断服务器端口全部开放或关闭"})
		return nil
	}
	policy := "无法识别"
	if summary.policy == "drop" {
		policy = "默认丢弃（drop）"
	}
	if summary.policy == "accept" {
		policy = "默认放行（accept）"
	}
	fields := []ui.CardField{
		{Label: "规则范围", Value: "本工具当前已加载的入站规则"},
		{Label: "默认策略", Value: policy},
	}
	if len(summary.otherInput) > 0 {
		fields = append(fields, ui.CardField{Label: "其他入站链", Value: strings.Join(summary.otherInput, "；"), Detail: "这些链仍可能进一步限制访问"})
	}
	if summary.problem != "" {
		fields = append(fields,
			ui.CardField{Label: "原因", Value: summary.problem},
			ui.CardField{Label: "查看详情", Value: "返回后选择 2 查看全部已加载规则"},
		)
		ui.PrintWarningCard("无法完整汇总端口", fields...)
		return nil
	}
	fields = append(fields, compactPortFields(summary)...)
	fields = append(fields,
		ui.CardField{Label: "内网 IPv4", Value: strings.Join(summary.private[0], ", ")},
		ui.CardField{Label: "内网 IPv6", Value: strings.Join(summary.private[1], ", ")},
	)
	var portRuleIDs []string
	for _, rule := range mergePortExceptions(summary.exceptions) {
		switch {
		case rule.sources == nil && !rule.hasSourcePorts && rule.hasProtocol:
			// These global accepts have already been unioned into the port
			// rows. Retain a reference to their IDs without repeating ports.
			if rule.id != "" && rule.id != "<nil>" {
				portRuleIDs = appendUnique(portRuleIDs, rule.id)
			}
		case rule.families == 2 && rule.protocols == 2 && rule.sources == nil && rule.hasPorts && rule.hasSourcePorts && rule.ports.String() == "546" && rule.sourcePorts.String() == "547":
			fields = append(fields, ui.CardField{Label: "DHCPv6 客户端", Value: "IPv6 UDP 目标 546（仅限源端口 547）"})
		default:
			label := "来源额外放行"
			if rule.sources == nil {
				label = "条件额外放行"
				if !rule.hasSourcePorts {
					label = "全协议放行"
				}
			}
			fields = append(fields, ui.CardField{Label: label, Value: rule.description()})
		}
	}
	if len(portRuleIDs) > 0 {
		fields = append(fields, ui.CardField{Label: "附加端口规则 ID/句柄", Value: strings.Join(portRuleIDs, ", ") + "（端口已汇总）"})
	}
	if len(summary.system) > 0 {
		fields = append(fields, ui.CardField{Label: "系统流量", Value: strings.Join(summary.system, "、")})
	}
	ui.PrintInfoCard("当前生效端口", fields...)
	fmt.Println("公网指内网范围外的来源；实际连通性还受服务监听、其他规则和云安全组影响。")
	return nil
}

// Merge equal rows only when they form a complete family/protocol rectangle.
// In particular, an equal L-shaped subset must never imply a fourth row.
func compactPortFields(summary loadedPortSummary) []ui.CardField {
	var fields []ui.CardField
	groups := [][]int{{0, 1, 2, 3}, {0, 2}, {1, 3}, {0, 1}, {2, 3}, {0}, {1}, {2}, {3}}
	for scope, label := range []string{"公网", "内网"} {
		var values [4]string
		for i := range values {
			values[i] = summary.ports[scope][i/2][i%2].String()
			if scope == 1 && len(summary.private[i/2]) == 0 {
				values[i] = "没有匹配的内网来源"
			}
		}
		var used [4]bool
		for _, group := range groups {
			matches := true
			for _, i := range group {
				if used[i] || values[i] != values[group[0]] {
					matches = false
				}
			}
			if !matches {
				continue
			}
			families, protocols := 0, 0
			for _, i := range group {
				used[i] = true
				families |= 1 << (i / 2)
				protocols |= 1 << (i % 2)
			}
			family := map[int]string{1: "IPv4", 2: "IPv6", 3: "IPv4 / IPv6"}[families]
			protocol := map[int]string{1: "TCP", 2: "UDP", 3: "TCP+UDP"}[protocols]
			fields = append(fields, ui.CardField{Label: label + " " + family + " " + protocol, Value: values[group[0]]})
		}
	}
	return fields
}
