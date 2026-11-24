package rules

import (
	"fmt"

	"snail_tool/internal/ui"
)

type Summary struct {
	Ports  [2][4]string
	Ranges []ui.CardField
}

func (summary Summary) Fields(ruleCount int, ruleErr error) []ui.CardField {
	var fields []ui.CardField
	for scope, label := range []string{"公网", "内网"} {
		values := summary.Ports[scope]
		if values[0] == values[1] && values[0] == values[2] && values[0] == values[3] {
			fields = append(fields, ui.CardField{Label: label, Value: "IPv4 / IPv6 · TCP / UDP → " + values[0]})
		} else if values[0] == values[2] && values[1] == values[3] {
			for i, protocol := range []string{"TCP", "UDP"} {
				fields = append(fields, ui.CardField{Label: label + " " + protocol, Value: "IPv4 / IPv6 → " + values[i]})
			}
		} else {
			for family, version := range []string{"IPv4", "IPv6"} {
				if values[family*2] == values[family*2+1] {
					fields = append(fields, ui.CardField{Label: label + " " + version, Value: "TCP / UDP → " + values[family*2]})
				} else {
					for protocol, name := range []string{"TCP", "UDP"} {
						fields = append(fields, ui.CardField{Label: label + " " + version + " " + name, Value: values[family*2+protocol]})
					}
				}
			}
		}
	}
	fields = append(fields, summary.Ranges...)
	if ruleErr != nil {
		return append(fields, ui.CardField{Label: "规则配置", Value: ruleErr.Error()})
	}
	return append(fields, ui.CardField{Label: "规则配置", Value: fmt.Sprintf("%d 条", ruleCount), Detail: "可匹配来源、目标、网卡和端口；拒绝默认排在放行前面"})
}
