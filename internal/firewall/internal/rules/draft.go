package rules

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"snail_tool/internal/ui"
)

func Summarize(content string, lists []PolicyList) Summary {
	var summary Summary
	for i, list := range lists[:8] {
		value, err := ReadPolicyValue(content, list)
		if err != nil {
			value = err.Error()
		} else if value == "" {
			value = "无"
		}
		summary.Ports[i/4][i%4] = value
	}
	for _, list := range lists[8:] {
		value, err := ReadPolicyValue(content, list)
		if err != nil {
			value = err.Error()
		}
		summary.Ranges = append(summary.Ranges, ui.CardField{Label: list.Label, Value: value})
	}
	return summary
}

func (session Session) ruleChangeFields(before, after string) []ui.CardField {
	old, oldErr := ManagedRulesIn(before, session.block)
	next, nextErr := ManagedRulesIn(after, session.block)
	if oldErr != nil || nextErr != nil || reflect.DeepEqual(old, next) {
		return nil
	}
	previous, current := make(map[string]Rule), make(map[string]Rule)
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
			fields = append(fields, ui.CardField{Label: "删除放行规则 " + rule.ID, Value: rule.Description()})
		}
	}
	for _, rule := range next {
		prior, exists := previous[rule.ID]
		if !exists {
			fields = append(fields, ui.CardField{Label: "添加放行规则 " + rule.ID, Value: rule.Description()})
		} else if prior != rule {
			fields = append(fields, ui.CardField{Label: "编辑放行规则 " + rule.ID, Value: rule.Description(), Detail: "原设置：" + prior.Description()})
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

func (session Session) EditSourceRulesDraft(view *ui.UI, original string) (string, error) {
	draft := original
	for {
		rules, err := ManagedRulesIn(draft, session.block)
		if err != nil {
			return "", err
		}
		session.sourceRulePage("", Rule{})
		ui.PrintInfoCard("规则配置",
			ui.CardField{Label: "匹配顺序", Value: "从上到下，先命中的规则生效"},
			ui.CardField{Label: "保存方式", Value: "修改后返回" + session.title + "统一保存"},
		)
		PrintRuleOrder(rules, "")
		ui.MenuShortcutHint("n", "新建规则", "默认放行，拒绝会插到最前面")
		if len(rules) > 1 {
			ui.MenuShortcutHint("o", "调整规则顺序", "")
		}
		if len(rules) > 0 {
			ui.MenuShortcutHint("f", "按端口查找并删除规则", "")
		}
		ui.MenuExit("0/q", "返回"+session.title)
		prompt := "请选择："
		if len(rules) > 0 {
			prompt = "请输入规则序号、完整 ID 或操作："
		}
		choice, err := view.Ask(prompt)
		if err != nil {
			return "", err
		}
		if ui.IsReturnChoice(choice) {
			return draft, nil
		}
		if index := FindRuleIndex(rules, choice); index >= 0 {
			draft, err = session.editSourceRuleDetails(view, draft, index)
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
		ui.RunAction(view, "修改规则配置失败，已返回规则配置", func() error {
			var updated []Rule
			var err error
			switch choice {
			case "n":
				updated, err = session.ChangeAllowRuleDraft(view, rules, "")
			case "o":
				updated, err = session.ReorderAllowRulesDraft(view, rules)
			case "f":
				updated, err = session.DeleteRuleByPortDraft(view, rules)
			}
			if err == nil && !reflect.DeepEqual(rules, updated) {
				var next string
				next, err = ReplaceManagedRulesIn(draft, updated, session.block)
				if err == nil {
					draft = next
				}
			}
			return err
		})
	}
}

func (session Session) editSourceRuleDetails(view *ui.UI, draft string, index int) (string, error) {
	for {
		rules, err := ManagedRulesIn(draft, session.block)
		if err != nil {
			return "", err
		}
		rule := rules[index]
		session.sourceRulePage(fmt.Sprintf("规则 %d", index+1), rule)
		ui.MenuOption("1", "编辑规则")
		ui.MenuOption("2", "删除规则")
		options := map[string]string{"1": "edit", "2": "delete"}
		if rule.Kind == "port" && session.migrate != nil {
			ui.MenuOptionHint("3", "旧规则转入基础端口", "将端口加入公网和内网列表，再移除此规则")
			options["3"] = "migrate"
		}
		ui.MenuExit("0/q", "返回规则配置")
		action, err := ui.AskChoice(view, "请选择", "", options)
		if errors.Is(err, ui.ErrReturnToMenu) {
			return draft, nil
		}
		if err != nil {
			return "", err
		}

		var updated string
		ui.RunAction(view, "修改规则配置失败", func() error {
			var err error
			switch action {
			case "migrate":
				converted, err := session.migrate(draft, rule.ID)
				if err != nil {
					return err
				}
				updated = converted
				ui.PrintInfoCard("转换已加入草稿", ui.CardField{Label: "生效方式", Value: "统一保存后按基础端口及来源分流匹配，仍受其他手写规则约束"})
			case "delete":
				updated, err = ReplaceManagedRulesIn(draft, session.deleteAllowRuleDraft(rules, index), session.block)
			case "edit":
				var next []Rule
				next, err = session.ChangeAllowRuleDraft(view, rules, rule.ID)
				if err == nil && !reflect.DeepEqual(rules, next) {
					updated, err = ReplaceManagedRulesIn(draft, next, session.block)
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
