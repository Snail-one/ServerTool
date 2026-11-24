package firewall

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

func summarySSHPorts(content string) string {
	text, err := sshPortText(content)
	if err != nil {
		return err.Error()
	}
	return text
}

// Summaries merge equal policies, while the editor still keeps all eight
// scopes independent. Each list shows the ports it actually contains.
func accessSummaryFields(content string) []ui.CardField {
	var fields []ui.CardField
	for scope, label := range []string{"公网", "内网"} {
		var values [4]string
		for i := range values {
			value, err := readPolicyValue(content, policyLists[scope*4+i])
			if err != nil {
				values[i] = err.Error()
				continue
			}
			if value == "" {
				value = "无"
			}
			values[i] = value
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
		return append(fields, ui.CardField{Label: "规则配置", Value: err.Error()})
	}
	legacy := 0
	for _, rule := range rules {
		if rule.Kind == "port" {
			legacy++
		}
	}
	fields = append(fields,
		ui.CardField{Label: "规则配置", Value: fmt.Sprintf("%d 条", len(rules)-legacy), Detail: "可匹配来源、目标、网卡和端口；拒绝默认排在放行前面"},
		ui.CardField{Label: "SSH 端口", Value: summarySSHPorts(content), Detail: "当前 SSH 服务端口。仍包含旧端口的入站名单会随 SSH 安全配置更新；从名单删除后不会自动加回"},
	)
	if legacy > 0 {
		fields = append(fields, ui.CardField{Label: "旧任意来源规则", Value: fmt.Sprintf("已有 %d 条，仍可额外放行公网和内网端口", legacy), Detail: "进入规则配置查看、删除或转入基础端口；基础列表不代表最终放行范围"})
	}
	return fields
}

func forwardSummaryFields(content string) []ui.CardField {
	var fields []ui.CardField
	for scope, label := range []string{"公网", "内网"} {
		var values [4]string
		for i := range values {
			value, err := readPolicyValue(content, forwardPolicyLists[scope*4+i])
			if err != nil {
				values[i] = err.Error()
				continue
			}
			if value == "" {
				value = "无"
			}
			values[i] = value
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
	for _, list := range forwardPolicyLists[8:] {
		value, err := readPolicyValue(content, list)
		if err != nil {
			value = err.Error()
		}
		fields = append(fields, ui.CardField{Label: list.label, Value: value})
	}
	rules, err := managedRulesIn(content, forwardRuleBlock)
	if err != nil {
		return append(fields, ui.CardField{Label: "规则配置", Value: err.Error()})
	}
	fields = append(fields, ui.CardField{Label: "规则配置", Value: fmt.Sprintf("%d 条", len(rules)), Detail: "可匹配来源、目标、网卡和端口；拒绝默认排在放行前面"})
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
			fields = append(fields, ui.CardField{Label: "删除放行规则 " + rule.ID, Value: rule.description()})
		}
	}
	for _, rule := range next {
		prior, exists := previous[rule.ID]
		if !exists {
			fields = append(fields, ui.CardField{Label: "添加放行规则 " + rule.ID, Value: rule.description()})
		} else if prior != rule {
			fields = append(fields, ui.CardField{Label: "编辑放行规则 " + rule.ID, Value: rule.description(), Detail: "原设置：" + prior.description()})
		}
	}
	if !reflect.DeepEqual(oldIDs, nextIDs) {
		value, prior := strings.Join(nextIDs, " → "), strings.Join(oldIDs, " → ")
		if value == "" {
			value = "无规则配置"
		}
		if prior == "" {
			prior = "无规则配置"
		}
		fields = append(fields, ui.CardField{Label: "放行规则顺序", Value: value, Detail: "原顺序：" + prior})
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
		sourceRulePage("", allowRule{})
		ui.PrintInfoCard("规则配置",
			ui.CardField{Label: "匹配顺序", Value: "从上到下，先命中的规则生效"},
			ui.CardField{Label: "保存方式", Value: "修改后返回" + sessionTitle() + "统一保存"},
		)
		printRuleOrder(rules, "")
		ui.MenuShortcutHint("n", "新建规则", "默认放行，拒绝会插到最前面")
		if len(rules) > 1 {
			ui.MenuShortcutHint("o", "调整规则顺序", "")
		}
		if len(rules) > 0 {
			ui.MenuShortcutHint("f", "按端口查找并删除规则", "")
		}
		ui.MenuExit("0/q", "返回"+sessionTitle())
		prompt := "请选择："
		if len(rules) > 0 {
			prompt = "请输入规则序号、完整 ID 或操作："
		}
		choice, err := view.Ask(prompt)
		if err != nil {
			return "", err
		}
		if shared.IsReturnChoice(choice) {
			return draft, nil
		}
		if index := findRuleIndex(rules, choice); index >= 0 {
			draft, err = editSourceRuleDetails(view, draft, index)
			if err != nil {
				return "", err
			}
			continue
		}
		choice = strings.ToLower(choice)
		if choice != "n" && (choice != "o" || len(rules) < 2) && (choice != "f" || len(rules) == 0) {
			ui.InvalidChoice()
			continue
		}
		shared.RunAction(view, "修改规则配置失败，已返回规则配置", func() error {
			var updated []allowRule
			var err error
			switch choice {
			case "n":
				updated, err = changeAllowRuleDraft(view, rules, "")
			case "o":
				updated, err = reorderAllowRulesDraft(view, rules)
			case "f":
				updated, err = deleteRuleByPortDraft(view, rules)
			}
			if err == nil && !reflect.DeepEqual(rules, updated) {
				var next string
				next, err = replaceManagedRules(draft, updated)
				if err == nil {
					draft = next
				}
			}
			return err
		})
	}
}

func editSourceRuleDetails(view *ui.UI, draft string, index int) (string, error) {
	for {
		rules, err := managedRules(draft)
		if err != nil {
			return "", err
		}
		rule := rules[index]
		sourceRulePage(fmt.Sprintf("规则 %d", index+1), rule)
		ui.MenuOption("1", "编辑规则")
		ui.MenuOption("2", "删除规则")
		options := map[string]string{"1": "edit", "2": "delete"}
		if rule.Kind == "port" && (activeSession == nil || !activeSession.forward) {
			ui.MenuOptionHint("3", "旧规则转入基础端口", "将端口加入公网和内网列表，再移除此规则")
			options["3"] = "migrate"
		}
		ui.MenuExit("0/q", "返回规则配置")
		action, err := askRuleChoice(view, "请选择", "", options)
		if errors.Is(err, shared.ErrReturnToMenu) {
			return draft, nil
		}
		if err != nil {
			return "", err
		}

		var updated string
		shared.RunAction(view, "修改规则配置失败", func() error {
			var err error
			switch action {
			case "migrate":
				converted, err := migrateGlobalPortRule(draft, rule.ID)
				if err != nil {
					return err
				}
				updated = converted
				ui.PrintInfoCard("转换已加入草稿", ui.CardField{Label: "生效方式", Value: "统一保存后按基础端口及来源分流匹配，仍受其他手写规则约束"})
			case "delete":
				updated, err = replaceManagedRules(draft, deleteAllowRuleDraft(rules, index))
			case "edit":
				var next []allowRule
				next, err = changeAllowRuleDraft(view, rules, rule.ID)
				if err == nil && !reflect.DeepEqual(rules, next) {
					updated, err = replaceManagedRules(draft, next)
				}
			}
			return err
		})
		if updated != "" {
			draft = updated
			if action != "edit" {
				return draft, nil
			}
		}
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
