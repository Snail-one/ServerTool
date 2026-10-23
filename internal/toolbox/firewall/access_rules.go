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
		return append(fields, ui.CardField{Label: "来源 IP 放行", Value: err.Error()})
	}
	legacy := 0
	for _, rule := range rules {
		if rule.Kind == "port" {
			legacy++
		}
	}
	fields = append(fields,
		ui.CardField{Label: "来源 IP 放行", Value: fmt.Sprintf("%d 条", len(rules)-legacy), Detail: "按来源 IP / 网段设置端口和协议，优先于基础端口放行"},
		ui.CardField{Label: "SSH 端口", Value: policyPortsText(sshPorts), Detail: "自动保留，通过 SSH 安全配置修改"},
	)
	if legacy > 0 {
		fields = append(fields, ui.CardField{Label: "旧任意来源规则", Value: fmt.Sprintf("已有 %d 条，仍可额外放行公网和内网端口", legacy), Detail: "进入来源 IP 放行查看、删除或转入基础端口；基础列表不代表最终放行范围"})
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
			value = "无来源 IP 放行"
		}
		if prior == "" {
			prior = "无来源 IP 放行"
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
		ui.PrintInfoCard("来源 IP 放行草稿",
			ui.CardField{Label: "配置对象", Value: "访问本机的来源 IP / 网段（客户端地址）", Detail: "例如：允许 192.168.1.10 访问 TCP 3306"},
			ui.CardField{Label: "放行权限", Value: "指定端口，或所选 TCP / UDP 协议的全部端口"},
			ui.CardField{Label: "匹配方式", Value: "优先于公网 / 内网基础端口放行"},
			ui.CardField{Label: "保存方式", Value: "本页只改草稿，返回访问规则管理后统一保存"},
		)
		printRuleOrder(rules, "")
		legacy := false
		for _, rule := range rules {
			if rule.Kind == "port" {
				legacy = true
				break
			}
		}
		ui.MenuOptionHint("1", "添加来源 IP", "填写 IP / 网段，再设置允许的端口和协议")
		ui.MenuOption("2", "编辑放行规则")
		ui.MenuOption("3", "删除放行规则")
		ui.MenuOption("4", "调整放行规则顺序")
		if legacy {
			ui.MenuOptionHint("5", "旧规则转入基础端口", "将所选规则的端口加入公网和内网列表，再移除此规则")
		}
		ui.MenuExit("0/q", "返回访问规则草稿")
		choice, err := view.Ask("请选择：")
		if err != nil {
			return "", err
		}
		if shared.IsReturnChoice(choice) {
			return draft, nil
		}
		if choice != "1" && choice != "2" && choice != "3" && choice != "4" && (choice != "5" || !legacy) {
			ui.InvalidChoice()
			continue
		}
		shared.RunAction(view, "修改来源 IP 放行失败，已返回规则草稿", func() error {
			updated, err := changeSourceRulesDraft(view, draft, rules, choice)
			if err == nil {
				draft = updated
			}
			return err
		})
	}
}

// Every source-rule action returns a new draft. The access manager owns the
// only save/load boundary, so subpages cannot apply an incomplete edit.
func changeSourceRulesDraft(view *ui.UI, content string, rules []allowRule, choice string) (string, error) {
	var updated []allowRule
	var err error
	switch choice {
	case "1", "2":
		id := ""
		if choice == "2" {
			sourceRulePage("选择编辑放行规则", allowRule{})
			printRuleOrder(rules, "")
			ui.MenuExit("0/q", "取消编辑，返回来源 IP 放行")
			id, err = chooseRuleID(view, rules)
			if err != nil {
				return "", err
			}
		}
		updated, err = changeAllowRuleDraft(view, rules, id)
	case "3":
		updated, err = deleteAllowRuleDraft(view, rules)
	case "4":
		updated, err = reorderAllowRulesDraft(view, rules)
	case "5":
		var candidates []allowRule
		for _, rule := range rules {
			if rule.Kind == "port" {
				candidates = append(candidates, rule)
			}
		}
		sourceRulePage("旧规则转入基础端口", allowRule{})
		ui.PrintInfoCard("选择转换规则", ui.CardField{Label: "转换范围", Value: "将所选端口加入公网和内网基础列表，再从草稿移除此规则"})
		printRuleOrder(candidates, "")
		ui.MenuExit("0/q", "取消转换，返回来源 IP 放行")
		id, err := chooseRuleID(view, candidates)
		if err != nil {
			return "", err
		}
		converted, err := migrateGlobalPortRule(content, id)
		if err == nil {
			ui.PrintInfoCard("转换已加入草稿", ui.CardField{Label: "生效方式", Value: "统一保存后按基础端口及来源分流匹配，仍受其他手写规则约束"})
		}
		return converted, err
	default:
		return "", fmt.Errorf("无效的来源 IP 放行操作")
	}
	if err != nil {
		return "", err
	}
	if reflect.DeepEqual(rules, updated) {
		return content, nil
	}
	return replaceManagedRules(content, updated)
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
