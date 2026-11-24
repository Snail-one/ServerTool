package access

import (
	"fmt"

	"snail_tool/internal/firewall/internal/rules"
	"snail_tool/internal/firewall/settings/sshaccess"
	"snail_tool/internal/ui"
)

const Title = "访问规则管理"
const Label = "访问规则"

func summaryFields(summary rules.Summary, count, legacy int, ssh string, rulesErr error) []ui.CardField {
	fields := summary.Fields(count, rulesErr)
	if rulesErr != nil {
		return fields
	}
	fields = append(fields, ui.CardField{Label: "SSH 端口", Value: ssh, Detail: "当前 SSH 服务端口。仍包含旧端口的入站名单会随 SSH 安全配置更新；从名单删除后不会自动加回"})
	if legacy > 0 {
		fields = append(fields, ui.CardField{Label: "旧任意来源规则", Value: fmt.Sprintf("已有 %d 条，仍可额外放行公网和内网端口", legacy), Detail: "进入规则配置查看、删除或转入基础端口；基础列表不代表最终放行范围"})
	}
	return fields
}

func summarySSHPorts(content string) string {
	text, err := sshaccess.PortText(content)
	if err != nil {
		return err.Error()
	}
	return text
}

func SummaryFields(content string) []ui.CardField {
	managed, err := rules.ManagedRules(content)
	legacy := 0
	for _, rule := range managed {
		if rule.Kind == "port" {
			legacy++
		}
	}
	ssh := ""
	if err == nil {
		ssh = summarySSHPorts(content)
	}
	return summaryFields(rules.Summarize(content, Lists), len(managed)-legacy, legacy, ssh, err)
}
