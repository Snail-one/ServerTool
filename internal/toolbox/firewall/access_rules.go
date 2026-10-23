package firewall

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

// Summaries merge equal policies, while the editor still keeps all eight
// scopes independent. SSH ports are expanded for a concrete port summary.
func accessSummaryFields(content string) []ui.CardField {
	var fields []ui.CardField
	ssh := sshDefinitionLine.FindStringSubmatch(content)
	var sshPorts portIntervals
	if len(ssh) > 1 {
		sshPorts, _ = parsePolicyPorts(ssh[1])
	}
	for scope, label := range []string{"公网", "内网"} {
		if len(ssh) < 2 {
			fields = append(fields, ui.CardField{Label: label, Value: "无法完整汇总，配置中的 SSH 端口格式未识别"})
			continue
		}
		var values [4]string
		for i := range values {
			value, err := readPolicyValue(content, policyLists[scope*4+i])
			if err != nil {
				values[i] = err.Error()
				continue
			}
			ports, _ := parsePolicyPorts(value)
			values[i] = policyPortsText(unionPorts(append(ports, sshPorts...)))
		}
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
	for _, list := range policyLists[8:] {
		value, err := readPolicyValue(content, list)
		if err != nil {
			value = err.Error()
		}
		fields = append(fields, ui.CardField{Label: list.label, Value: value})
	}
	rules, err := managedRules(content)
	if err != nil {
		return append(fields, ui.CardField{Label: "来源例外", Value: err.Error()})
	}
	legacy := 0
	for _, rule := range rules {
		if rule.Kind == "port" {
			legacy++
		}
	}
	fields = append(fields,
		ui.CardField{Label: "来源例外", Value: fmt.Sprintf("%d 条", len(rules)), Detail: "例外优先放行；内网名单决定使用哪组基础端口"},
		ui.CardField{Label: "SSH 端口", Value: policyPortsText(sshPorts), Detail: "自动保留，通过 SSH 安全配置修改"},
	)
	if legacy > 0 {
		fields = append(fields, ui.CardField{Label: "全来源例外", Value: fmt.Sprintf("已有 %d 条，仍可额外放行公网和内网端口", legacy), Detail: "进入来源例外查看、删除或转入基础端口；基础列表不代表最终放行范围"})
	}
	return fields
}

func ruleChangeFields(before, after string) []ui.CardField {
	old, oldErr := managedRules(before)
	next, nextErr := managedRules(after)
	if oldErr != nil || nextErr != nil || reflect.DeepEqual(old, next) {
		return nil
	}
	previous, current := make(map[string]allowRule), make(map[string]allowRule)
	var oldIDs, nextIDs []string
	for _, rule := range old {
		previous[rule.ID] = rule
		oldIDs = append(oldIDs, rule.ID)
	}
	for _, rule := range next {
		current[rule.ID] = rule
		nextIDs = append(nextIDs, rule.ID)
	}
	var fields []ui.CardField
	for _, rule := range old {
		if _, exists := current[rule.ID]; !exists {
			fields = append(fields, ui.CardField{Label: "删除例外 " + rule.ID, Value: rule.description()})
		}
	}
	for _, rule := range next {
		prior, exists := previous[rule.ID]
		if !exists {
			fields = append(fields, ui.CardField{Label: "添加例外 " + rule.ID, Value: rule.description()})
		} else if prior != rule {
			fields = append(fields, ui.CardField{Label: "编辑例外 " + rule.ID, Value: rule.description(), Detail: "原设置：" + prior.description()})
		}
	}
	if !reflect.DeepEqual(oldIDs, nextIDs) {
		value, prior := strings.Join(nextIDs, " → "), strings.Join(oldIDs, " → ")
		if value == "" {
			value = "无来源例外"
		}
		if prior == "" {
			prior = "无来源例外"
		}
		fields = append(fields, ui.CardField{Label: "例外顺序", Value: value, Detail: "原顺序：" + prior})
	}
	return fields
}

func editSourceRulesDraft(view *ui.UI, original string) (string, error) {
	draft := original
	for {
		rules, err := managedRules(draft)
		if err != nil {
			return "", err
		}
		ui.ClearScreen()
		ui.MenuTitle("防火墙管理", "访问规则管理", "来源例外")
		ui.PrintInfoCard("来源例外草稿",
			ui.CardField{Label: "匹配方式", Value: "优先于公网 / 内网基础端口放行"},
			ui.CardField{Label: "普通端口", Value: "通过公网 / 内网端口设置；本页为指定来源额外放行"},
			ui.CardField{Label: "保存方式", Value: "本页只改草稿，返回访问规则管理后统一保存"},
		)
		printRuleOrder(rules, "")
		legacy := false
		for _, rule := range rules {
			if rule.Kind == "port" {
				legacy = true
				ui.PrintField("已有全来源例外 "+rule.ID, rule.description())
			}
		}
		ui.MenuOption("1", "添加来源例外")
		ui.MenuOption("2", "编辑例外")
		ui.MenuOption("3", "删除例外")
		ui.MenuOption("4", "调整例外顺序")
		if legacy {
			ui.MenuOptionHint("5", "全来源例外转入基础端口", "将所选规则的端口加入公网和内网列表，再移除此例外")
		}
		ui.MenuExit("0/q", "返回访问规则草稿")
		choice, err := view.Ask("请选择：")
		if err != nil {
			return "", err
		}
		if shared.IsReturnChoice(choice) {
			return draft, nil
		}
		editor := ruleEditor{pending: true, save: func(updated []allowRule) error {
			content, err := replaceManagedRules(draft, updated)
			if err == nil {
				draft = content
			}
			return err
		}}
		var action func() error
		switch choice {
		case "1":
			action = func() error { return changeAllowRuleWithEditor(view, rules, "", editor) }
		case "2":
			action = func() error {
				id, err := chooseRuleID(view, rules)
				if err != nil {
					return err
				}
				return changeAllowRuleWithEditor(view, rules, id, editor)
			}
		case "3":
			action = func() error { return deleteAllowRuleWithEditor(view, rules, editor) }
		case "4":
			action = func() error { return reorderAllowRulesWithEditor(view, rules, editor) }
		case "5":
			if legacy {
				action = func() error {
					var candidates []allowRule
					for _, rule := range rules {
						if rule.Kind == "port" {
							candidates = append(candidates, rule)
						}
					}
					printRuleOrder(candidates, "")
					id, err := chooseRuleID(view, candidates)
					if err != nil {
						return err
					}
					updated, err := migrateGlobalPortRule(draft, id)
					if err == nil {
						draft = updated
						ui.PrintInfoCard("转换已加入草稿", ui.CardField{Label: "生效方式", Value: "统一保存后按基础端口及来源分流匹配，仍受其他手写规则约束"})
					}
					return err
				}
			}
		}
		if action == nil {
			ui.InvalidChoice()
			continue
		}
		shared.RunAction(view, "修改来源例外失败，已返回例外草稿", action)
	}
}

func migrateGlobalPortRule(content, id string) (string, error) {
	rules, err := managedRules(content)
	if err != nil {
		return "", err
	}
	var target allowRule
	var remaining []allowRule
	for _, rule := range rules {
		if rule.ID == id {
			target = rule
		} else {
			remaining = append(remaining, rule)
		}
	}
	if target.Kind != "port" {
		return "", errors.New("请选择已有的全来源端口例外")
	}
	updated := content
	for i, list := range policyLists[:8] {
		family := 1
		if i%4 >= 2 {
			family = 2
		}
		protocol := "tcp"
		if i%2 == 1 {
			protocol = "udp"
		}
		if target.familyMask()&family == 0 || target.Protocol != "both" && target.Protocol != protocol {
			continue
		}
		current, err := readPolicyValue(updated, list)
		if err != nil {
			return "", err
		}
		value, err := changePolicyValue(list, current, "add", target.Ports, -1)
		if err != nil {
			return "", err
		}
		updated, err = withPolicyValue(updated, list, value)
		if err != nil {
			return "", err
		}
	}
	return replaceManagedRules(updated, remaining)
}
