package firewall

import (
	"fmt"
	"strconv"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

func chooseRuleID(view *ui.UI, rules []allowRule) (string, error) {
	if len(rules) == 0 {
		return "", fmt.Errorf("没有可选择的规则")
	}
	for {
		id, err := view.Ask("请输入列表序号或完整规则 ID（0/q 返回）：")
		if err != nil {
			return "", err
		}
		if shared.IsReturnChoice(id) {
			return "", shared.ErrReturnToMenu
		}
		for _, rule := range rules {
			if strings.EqualFold(id, rule.ID) {
				return rule.ID, nil
			}
		}
		if number, err := strconv.Atoi(id); err == nil && number >= 1 && number <= len(rules) {
			return rules[number-1].ID, nil
		}
		fmt.Println("未找到该序号或规则 ID，请从当前列表选择")
	}
}

func askRuleChoice(view *ui.UI, prompt, fallback string, choices map[string]string) (string, error) {
	if !strings.HasSuffix(prompt, "：") && !strings.HasSuffix(prompt, ":") {
		prompt += "："
	}
	for {
		value, err := view.Ask(prompt)
		if err != nil {
			return "", err
		}
		if shared.IsReturnChoice(value) {
			return "", shared.ErrReturnToMenu
		}
		if value == "" {
			value = fallback
		}
		if choice, ok := choices[strings.ToLower(value)]; ok {
			return choice, nil
		}
		ui.InvalidChoice()
	}
}

func askRuleSource(view *ui.UI, current, family string) (string, error) {
	ui.PrintField("来源说明", "填写访问本机的客户端 IP 或网段")
	example := "192.168.1.10 或 192.168.1.0/24"
	if family == "ipv6" {
		example = "2001:db8::10 或 2001:db8:1::/64"
	}
	ui.PrintField("填写示例", example)
	if current != "" && sourcePrefix(current).Addr().Is4() != (family == "ipv4") {
		current = ""
	}
	for {
		label := map[string]string{"ipv4": "IPv4", "ipv6": "IPv6"}[family]
		prompt := "请输入 " + label + " 来源 IP / CIDR（0/q 返回）："
		if current != "" {
			prompt = "请输入 " + label + " 来源 IP / CIDR（回车保留 " + current + "，0/q 返回）："
		}
		value, err := view.Ask(prompt)
		if err != nil {
			return "", err
		}
		if shared.IsReturnChoice(value) {
			return "", shared.ErrReturnToMenu
		}
		if value == "" {
			value = current
		}
		source, err := normalizeSource(value)
		if err != nil {
			log.Warn(err)
			continue
		}
		if sourcePrefix(source).Addr().Is4() != (family == "ipv4") {
			log.Warn("请输入与所选 ", label, " 版本一致的来源地址")
			continue
		}
		ui.PrintInfoCard("实际来源匹配范围",
			ui.CardField{Label: "IP 版本", Value: label},
			ui.CardField{Label: "输入地址", Value: value},
			ui.CardField{Label: "规范化地址", Value: source},
			ui.CardField{Label: "覆盖范围", Value: sourceRange(source)},
			ui.CardField{Label: "方向", Value: "该来源访问本服务器的入站流量"},
		)
		return source, nil
	}
}

func askRulePorts(view *ui.UI, current string) (string, error) {
	for {
		prompt := "请输入单端口或范围，例如 443 / 8000-8100（0/q 返回）："
		if current != "" {
			prompt = "请输入端口 / 范围（回车保留 " + current + "，0/q 返回）："
		}
		value, err := view.Ask(prompt)
		if err != nil {
			return "", err
		}
		if shared.IsReturnChoice(value) {
			return "", shared.ErrReturnToMenu
		}
		if value == "" {
			value = current
		}
		ports, _, _, err := parseRulePorts(value)
		if err == nil {
			return ports, nil
		}
		log.Warn(err)
	}
}

func askAllowRule(view *ui.UI, current allowRule) (allowRule, error) {
	return askAllowRuleType(view, current, false)
}

func askAllowRuleType(view *ui.UI, current allowRule, sourceOnly bool) (allowRule, error) {
	if sourceOnly && current.Kind != "port" {
		return askSourceIPRule(view, current)
	}
	rule := allowRule{ID: current.ID}
	sourceRulePage("选择放行类型", current)
	kinds := map[string]string{"1": "port", "2": "source", "3": "source_port"}
	fallback := map[string]string{"port": "1", "source": "2", "source_port": "3"}[current.Kind]
	if sourceOnly {
		kinds = map[string]string{"1": "source_port", "2": "source"}
		fallback = map[string]string{"source_port": "1", "source": "2", "port": "3"}[current.Kind]
		if fallback == "" {
			fallback = "1"
		}
		ruleChoiceOption("1", "来源 IP + 端口", "指定来源和端口 / 范围", fallback)
		ruleChoiceOption("2", "来源 IP 放行", "指定来源的全部 TCP / UDP 端口", fallback)
		if current.Kind == "port" {
			kinds["3"] = "port"
			ruleChoiceOption("3", "保留已有全来源端口例外", "也可通过转入基础端口移除此规则", fallback)
		}
	} else {
		if fallback == "" {
			fallback = "1"
		}
		ruleChoiceOption("1", "全来源端口例外", "优先放行；新端口请通过公网 / 内网端口设置", fallback)
		ruleChoiceOption("2", "来源 IP 放行", "指定来源的全部 TCP / UDP 端口", fallback)
		ruleChoiceOption("3", "来源 IP + 端口", "指定来源和端口 / 范围", fallback)
	}
	ui.MenuExit("0/q", "取消本次编辑，返回来源 IP 放行")
	if fallback == "" {
		fallback = "1"
	}
	kind, err := askRuleChoice(view, "请选择类型（回车默认 "+fallback+"）：", fallback, kinds)
	if err != nil {
		return rule, err
	}
	rule.Kind = kind
	rule, err = askRuleFamily(view, current, rule)
	if err != nil {
		return rule, err
	}
	if kind != "port" {
		rule, err = askRuleSources(view, current, rule)
		if err != nil {
			return rule, err
		}
	}
	if kind != "source" {
		sourceRulePage("填写端口", rule)
		ui.MenuExit("0/q", "取消本次编辑，返回来源 IP 放行")
		rule.Ports, err = askRulePorts(view, current.Ports)
		if err != nil {
			return rule, err
		}
	}
	rule, err = askRuleProtocol(view, current, rule)
	if err != nil {
		return rule, err
	}
	return normalizeAllowRule(rule)
}

func askRuleFamily(view *ui.UI, current, rule allowRule) (allowRule, error) {
	sourceRulePage("选择 IP 版本", rule)
	fallback := strconv.Itoa(current.familyMask())
	if current.Kind == "" {
		fallback = "1"
	}
	ruleChoiceOption("1", "IPv4", "", fallback)
	ruleChoiceOption("2", "IPv6", "", fallback)
	ruleChoiceOption("3", "IPv4 + IPv6", "", fallback)
	ui.MenuExit("0/q", "取消本次编辑，返回来源 IP 放行")
	family, err := askRuleChoice(view, "请选择 IP 版本（回车默认 "+fallback+"，0/q 返回）：", fallback,
		map[string]string{"1": "ipv4", "2": "ipv6", "3": "both", "ipv4": "ipv4", "ipv6": "ipv6", "ipv4+ipv6": "both", "both": "both"})
	if err != nil {
		return rule, err
	}
	rule.Family = family
	return rule, nil
}

func askRuleSources(view *ui.UI, current, rule allowRule) (allowRule, error) {
	var err error
	family := rule.Family
	if family == "both" {
		family = "ipv4"
	}
	currentSource := current.Source
	if family == "ipv6" && current.SourceIPv6 != "" {
		currentSource = current.SourceIPv6
	}
	sourceRulePage("填写来源地址", rule)
	ui.MenuExit("0/q", "取消本次编辑，返回来源 IP 放行")
	rule.Source, err = askRuleSource(view, currentSource, family)
	if err != nil {
		return rule, err
	}
	if rule.Family == "both" {
		currentSource = current.SourceIPv6
		if currentSource == "" && current.Source != "" && !sourcePrefix(current.Source).Addr().Is4() {
			currentSource = current.Source
		}
		sourceRulePage("填写 IPv6 来源地址", rule)
		ui.MenuExit("0/q", "取消本次编辑，返回来源 IP 放行")
		rule.SourceIPv6, err = askRuleSource(view, currentSource, "ipv6")
		if err != nil {
			return rule, err
		}
	}
	return rule, nil
}

func askRuleProtocol(view *ui.UI, current, rule allowRule) (allowRule, error) {
	sourceRulePage("选择协议", rule)
	fallback := map[string]string{"tcp": "1", "udp": "2", "both": "3"}[current.Protocol]
	if fallback == "" {
		fallback = "1"
	}
	ruleChoiceOption("1", "TCP", "", fallback)
	ruleChoiceOption("2", "UDP", "", fallback)
	ruleChoiceOption("3", "TCP + UDP", "", fallback)
	ui.MenuExit("0/q", "取消本次编辑，返回来源 IP 放行")
	protocol, err := askRuleChoice(view, "请选择协议（回车默认 "+fallback+"，0/q 返回）：", fallback, map[string]string{"1": "tcp", "2": "udp", "3": "both", "tcp": "tcp", "udp": "udp", "tcp+udp": "both"})
	if err != nil {
		return rule, err
	}
	rule.Protocol = protocol
	return rule, nil
}

func askSourceIPRule(view *ui.UI, current allowRule) (allowRule, error) {
	rule, err := askRuleFamily(view, current, allowRule{ID: current.ID})
	if err != nil {
		return rule, err
	}
	rule, err = askRuleSources(view, current, rule)
	if err != nil {
		return rule, err
	}
	sourceRulePage("设置访问权限", rule)
	fallback := "1"
	if current.Kind == "source" {
		fallback = "2"
	}
	ruleChoiceOption("1", "指定端口", "仅允许该来源访问填写的端口 / 范围", fallback)
	ruleChoiceOption("2", "全部端口", "允许该来源访问所选 TCP / UDP 协议的全部端口", fallback)
	ui.MenuExit("0/q", "取消本次编辑，返回来源 IP 放行")
	rule.Kind, err = askRuleChoice(view, "请选择放行范围（回车默认 "+fallback+"）", fallback,
		map[string]string{"1": "source_port", "2": "source"})
	if err != nil {
		return rule, err
	}
	if rule.Kind == "source_port" {
		sourceRulePage("填写允许访问的端口", rule)
		ui.MenuExit("0/q", "取消本次编辑，返回来源 IP 放行")
		rule.Ports, err = askRulePorts(view, current.Ports)
		if err != nil {
			return rule, err
		}
	}
	rule, err = askRuleProtocol(view, current, rule)
	if err != nil {
		return rule, err
	}
	return normalizeAllowRule(rule)
}

func changeAllowRule(view *ui.UI, version configVersion, rules []allowRule, id string) error {
	return changeAllowRuleWithEditor(view, rules, id, ruleEditor{save: func(updated []allowRule) error {
		return saveManagedRules(version, updated)
	}})
}

// ruleEditor shares validation and selection between draft editing and direct
// saves. Draft sessions only update their parent in memory.
type ruleEditor struct {
	pending bool
	save    func([]allowRule) error
}

func changeAllowRuleWithEditor(view *ui.UI, rules []allowRule, id string, editor ruleEditor) error {
	ui.ClearScreen()
	if id == "" {
		ui.MenuTitle("防火墙管理", "访问规则管理", "来源 IP 放行", "添加放行规则")
	} else {
		ui.MenuTitle("防火墙管理", "访问规则管理", "来源 IP 放行", "编辑放行规则")
	}
	current := allowRule{ID: id}
	if id == "" {
		var err error
		current.ID, err = newRuleID(rules)
		if err != nil {
			return err
		}
	} else {
		found := false
		for _, rule := range rules {
			if rule.ID == id {
				current, found = rule, true
			}
		}
		if !found {
			return fmt.Errorf("规则 ID 不存在，请刷新后重新选择")
		}
		ui.PrintInfoCard("编辑前的规则", ui.CardField{Label: current.ID, Value: current.description()})
	}
	draft, err := askAllowRuleType(view, current, editor.pending)
	if err != nil {
		return err
	}
	if draft == current {
		log.Info("规则未变化")
		return nil
	}
	warnings, err := checkRuleRelations(draft, rules)
	if err != nil {
		return err
	}
	title := "即将保存的入站放行规则"
	if editor.pending {
		title = "待加入草稿的来源 IP 放行"
	}
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "访问规则管理", "来源 IP 放行", "检查放行规则")
	ui.PrintInfoCard(title,
		ui.CardField{Label: "规则 ID", Value: draft.ID},
		ui.CardField{Label: "IP 版本", Value: draft.familyLabel()},
		ui.CardField{Label: "放行内容", Value: draft.description()},
		ui.CardField{Label: "实际来源范围", Value: draft.sourceRange()},
	)
	if len(warnings) > 0 {
		ui.PrintWarningCard("发现覆盖或重叠",
			ui.CardField{Label: "相关规则", Value: strings.Join(warnings, "\n")},
			ui.CardField{Label: "保存方式", Value: "保留各规则 ID，不合并、不删除其他规则"},
		)
	}
	if !editor.pending {
		confirmed, err := view.Confirm("确认保存此规则并立即加载？(y/N，0/q 返回)：")
		if err != nil {
			return err
		}
		if !confirmed {
			return shared.ErrReturnToMenu
		}
	}
	updated := append([]allowRule(nil), rules...)
	if id == "" {
		updated = append(updated, draft)
	} else {
		for i := range updated {
			if updated[i].ID == id {
				updated[i] = draft
			}
		}
	}
	if err := editor.save(updated); err != nil {
		return err
	}
	message := "放行规则已保存并加载"
	if editor.pending {
		message = "来源 IP 放行已加入草稿"
	}
	ui.PrintSuccessCard(message, ui.CardField{Label: draft.ID, Value: draft.description()})
	return nil
}

func queryRulesByPort(view *ui.UI, rules []allowRule) ([]allowRule, error) {
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "访问规则管理", "来源 IP 放行", "按端口查询")
	ui.MenuExit("0/q", "取消查询，返回来源 IP 放行")
	ports, err := askRulePorts(view, "")
	if err != nil {
		return nil, err
	}
	_, start, end, _ := parseRulePorts(ports)
	var matches []allowRule
	for _, rule := range rules {
		a, b := rule.portBounds()
		if a <= end && start <= b {
			matches = append(matches, rule)
		}
	}
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "访问规则管理", "来源 IP 放行", "端口查询结果")
	ui.PrintInfoCard("端口查询结果",
		ui.CardField{Label: "端口 / 范围", Value: ports},
		ui.CardField{Label: "查询范围", Value: "IPv4 / IPv6、全部协议；IP 放行规则涵盖全部端口，也会列出"},
	)
	printRuleOrder(matches, "")
	return matches, nil
}

func deleteAllowRule(view *ui.UI, version configVersion, rules []allowRule) error {
	return deleteAllowRuleWithEditor(view, rules, ruleEditor{save: func(updated []allowRule) error {
		return saveManagedRules(version, updated)
	}})
}

func deleteAllowRuleWithEditor(view *ui.UI, rules []allowRule, editor ruleEditor) error {
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "访问规则管理", "来源 IP 放行", "删除放行规则")
	ruleChoiceOption("1", "按序号 / 规则 ID 删除", "", "1")
	ui.MenuOption("2", "按端口查询后选择规则删除")
	ui.MenuExit("0/q", "取消删除，返回来源 IP 放行")
	mode, err := askRuleChoice(view, "请选择删除方式（回车默认 1）：", "1", map[string]string{"1": "id", "2": "port"})
	if err != nil {
		return err
	}
	candidates := rules
	if mode == "port" {
		candidates, err = queryRulesByPort(view, rules)
		if err != nil {
			return err
		}
	} else {
		sourceRulePage("选择删除放行规则", allowRule{})
		printRuleOrder(candidates, "")
	}
	ui.MenuExit("0/q", "取消删除，返回来源 IP 放行")
	id, err := chooseRuleID(view, candidates)
	if err != nil {
		return err
	}
	var target allowRule
	var updated []allowRule
	for _, rule := range rules {
		if rule.ID == id {
			target = rule
		} else {
			updated = append(updated, rule)
		}
	}
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "访问规则管理", "来源 IP 放行", "检查删除范围")
	ui.PrintWarningCard("即将删除一条放行规则",
		ui.CardField{Label: "规则 ID", Value: target.ID},
		ui.CardField{Label: "IP 版本", Value: target.familyLabel()},
		ui.CardField{Label: "放行内容", Value: target.description()},
		ui.CardField{Label: "实际来源范围", Value: target.sourceRange()},
		ui.CardField{Label: "删除范围", Value: "仅删除此 ID；其他放行规则及基础端口保持，仍可能允许相同流量"},
	)
	for _, rule := range updated {
		if target.overlaps(rule) {
			ui.PrintField("仍保留的重叠规则 "+rule.ID, rule.description())
		}
	}
	if !editor.pending {
		confirmed, err := view.Confirm("确认仅删除规则 " + id + " 并立即加载？(y/N，0/q 返回)：")
		if err != nil {
			return err
		}
		if !confirmed {
			return shared.ErrReturnToMenu
		}
	}
	if err := editor.save(updated); err != nil {
		return err
	}
	message := "指定规则已删除并加载"
	if editor.pending {
		message = "删除操作已加入草稿"
	}
	ui.PrintSuccessCard(message, ui.CardField{Label: "规则 ID", Value: id})
	return nil
}
