package rules

import (
	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/ui"
)

func (session Session) sourceRulePage(stage string, rule Rule) {
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", session.title, "规则配置", stage)
	var fields []ui.CardField
	if rule.ID != "" {
		fields = append(fields, ui.CardField{Label: "规则 ID", Value: rule.ID})
	}
	if label := map[string]string{"port": "任意来源（旧规则）", "source": "全部端口（按协议）", "source_port": "指定端口"}[rule.Kind]; label != "" {
		fields = append(fields, ui.CardField{Label: "放行类型", Value: label})
	}
	if rule.Family != "" {
		fields = append(fields, ui.CardField{Label: "IP 版本", Value: rule.FamilyLabel()})
	}
	if rule.Source != "" {
		fields = append(fields, ui.CardField{Label: "来源", Value: rule.SourceLabel(), Detail: rule.SourceRange()})
	}
	if rule.Dest != "" || rule.DestIPv6 != "" {
		dest := rule.Dest
		if rule.DestIPv6 != "" {
			if dest != "" {
				dest += " / "
			}
			dest += rule.DestIPv6
		}
		fields = append(fields, ui.CardField{Label: "目标", Value: dest})
	}
	if rule.Iif != "" {
		fields = append(fields, ui.CardField{Label: "来源网卡", Value: rule.Iif})
	}
	if rule.Oif != "" {
		fields = append(fields, ui.CardField{Label: "目标网卡", Value: rule.Oif})
	}
	if rule.Sport != "" {
		fields = append(fields, ui.CardField{Label: "来源端口", Value: rule.Sport})
	}
	if rule.Ports != "" {
		fields = append(fields, ui.CardField{Label: "端口", Value: rule.Ports})
	}
	if rule.Protocol != "" {
		fields = append(fields, ui.CardField{Label: "协议", Value: configuration.ProtocolLabel(rule.Protocol)})
	}
	if rule.Verdict == "drop" {
		fields = append(fields, ui.CardField{Label: "动作", Value: "拒绝"})
	}
	if len(fields) > 0 {
		ui.PrintInfoCard("当前选择", fields...)
	}
}

func ruleChoiceOption(key, label, hint, fallback string) {
	if key == fallback {
		if hint != "" {
			hint += "；"
		}
		hint += "默认，直接回车选择"
	}
	ui.MenuOptionHint(key, label, hint)
}
