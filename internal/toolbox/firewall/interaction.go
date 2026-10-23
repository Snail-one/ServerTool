package firewall

import "snail_tool/internal/ui"

func sourceRulePage(stage string, rule allowRule) {
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "访问规则管理", "来源 IP 放行", stage)
	var fields []ui.CardField
	if rule.ID != "" {
		fields = append(fields, ui.CardField{Label: "规则 ID", Value: rule.ID})
	}
	if label := map[string]string{"port": "任意来源（旧规则）", "source": "全部端口（按协议）", "source_port": "指定端口"}[rule.Kind]; label != "" {
		fields = append(fields, ui.CardField{Label: "放行类型", Value: label})
	}
	if rule.Family != "" {
		fields = append(fields, ui.CardField{Label: "IP 版本", Value: rule.familyLabel()})
	}
	if rule.Source != "" {
		fields = append(fields, ui.CardField{Label: "来源", Value: rule.sourceLabel(), Detail: rule.sourceRange()})
	}
	if rule.Ports != "" {
		fields = append(fields, ui.CardField{Label: "端口", Value: rule.Ports})
	}
	if rule.Protocol != "" {
		fields = append(fields, ui.CardField{Label: "协议", Value: protocolLabel(rule.Protocol)})
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
