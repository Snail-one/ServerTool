package firewall

import (
	"fmt"
	"strconv"
	"strings"

	"snail_tool/internal/input"
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
		if number, err := input.Number(id, 1, len(rules)); err == nil && number >= 1 && number <= len(rules) {
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
	hint := "填写访问本机的客户端 IP 或网段"
	if activeSession != nil && activeSession.forward {
		hint = "填写经本机转发的来源 IP 或网段"
	}
	ui.PrintField("来源说明", hint)
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
			log.Warn("请输入与所选 ", label, " 版本一致的地址")
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

func askOptionalSource(view *ui.UI, current, family string, required bool) (string, error) {
	for {
		label := map[string]string{"ipv4": "IPv4", "ipv6": "IPv6"}[family]
		prompt := "请输入 " + label + " 地址 / CIDR（回车跳过，- 清除，0/q 返回）："
		if required && current != "" {
			prompt = "请输入 " + label + " 地址 / CIDR（回车保留 " + current + "，0/q 返回）："
		} else if required {
			prompt = "请输入 " + label + " 地址 / CIDR（0/q 返回）："
		} else if current != "" {
			prompt = "请输入 " + label + " 地址 / CIDR（回车保留 " + current + "，- 清除，0/q 返回）："
		}
		value, err := view.Ask(prompt)
		if err != nil {
			return "", err
		}
		if shared.IsReturnChoice(value) {
			return "", shared.ErrReturnToMenu
		}
		if value == "-" {
			return "", nil
		}
		if value == "" {
			if current != "" {
				return current, nil
			}
			if required {
				log.Warn("这一项需要填写")
				continue
			}
			return "", nil
		}
		source, err := normalizeSource(value)
		if err != nil {
			log.Warn(err)
			continue
		}
		if sourcePrefix(source).Addr().Is4() != (family == "ipv4") {
			log.Warn("请输入与所选 ", label, " 版本一致的地址")
			continue
		}
		return source, nil
	}
}

func askOptionalText(view *ui.UI, current, label string, normalize func(string) (string, error)) (string, error) {
	for {
		prompt := "请输入" + label + "（回车跳过，- 清除，0/q 返回）："
		if current != "" {
			prompt = "请输入" + label + "（回车保留 " + current + "，- 清除，0/q 返回）："
		}
		value, err := view.Ask(prompt)
		if err != nil {
			return "", err
		}
		if shared.IsReturnChoice(value) {
			return "", shared.ErrReturnToMenu
		}
		if value == "-" {
			return "", nil
		}
		if value == "" {
			return current, nil
		}
		normalized, err := normalize(value)
		if err != nil {
			log.Warn(err)
			continue
		}
		return normalized, nil
	}
}

func askOptionalPorts(view *ui.UI, current, label string) (string, error) {
	for {
		prompt := "请输入" + label + "（回车跳过，- 清除，0/q 返回）："
		if current != "" {
			prompt = "请输入" + label + "（回车保留 " + current + "，- 清除，0/q 返回）："
		}
		value, err := view.Ask(prompt)
		if err != nil {
			return "", err
		}
		if shared.IsReturnChoice(value) {
			return "", shared.ErrReturnToMenu
		}
		if value == "-" {
			return "", nil
		}
		if value == "" {
			return current, nil
		}
		ports, _, _, err := parseRulePorts(value)
		if err != nil {
			log.Warn(err)
			continue
		}
		return ports, nil
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

// New rules collect a full match. Enter skips an empty condition. Legacy
// any-source port rules stay on their original shorter path.
func askAllowRule(view *ui.UI, current allowRule) (allowRule, error) {
	rule := current
	legacy := current.Kind == "port"
	err := ui.RunPromptSteps(
		ui.PromptStep{Enabled: func() bool { return legacy }, Run: func() error {
			sourceRulePage("选择放行类型", rule)
			ruleChoiceOption("1", "改为完整规则", "可以填写来源、目标、网卡和端口", "3")
			ruleChoiceOption("2", "改为完整规则且不限端口", "去掉这条旧规则上的端口", "3")
			ruleChoiceOption("3", "保留已有全来源端口例外", "也可通过转入基础端口移除此规则", "3")
			ui.MenuExit("0/q", "返回上一步")
			kind, err := askRuleChoice(view, "请选择类型（回车默认 3）", "3", map[string]string{"1": "source_port", "2": "source", "3": "port"})
			if err == nil {
				rule.Kind = kind
				if kind == "port" {
					rule.Source, rule.SourceIPv6 = "", ""
				}
				if kind == "source" {
					rule.Ports = ""
				}
			}
			return err
		}},
		ui.PromptStep{Enabled: func() bool { return rule.Kind != "port" }, Run: func() error {
			sourceRulePage("选择动作", rule)
			fallback := "1"
			if current.Verdict == "drop" {
				fallback = "2"
			}
			ruleChoiceOption("1", "放行", "命中后直接放行", fallback)
			ruleChoiceOption("2", "拒绝", "命中后丢弃，新规则默认排在最前", fallback)
			ui.MenuExit("0/q", "返回上一步")
			verdict, err := askRuleChoice(view, "请选择动作（回车默认 "+fallback+"）", fallback, map[string]string{"1": "accept", "2": "drop"})
			if err == nil && verdict == "drop" {
				rule.Verdict = "drop"
			} else if err == nil {
				rule.Verdict = ""
			}
			return err
		}},
		ui.PromptStep{Run: func() error {
			updated, err := askRuleFamily(view, current, rule)
			if err == nil {
				rule = updated
			}
			return err
		}},
		ui.PromptStep{Enabled: func() bool { return rule.Kind != "port" }, Run: func() error {
			value, err := askMatchSource(view, current, rule, false)
			if err == nil {
				rule.Source, rule.SourceIPv6 = value, ""
			}
			return err
		}},
		ui.PromptStep{Enabled: func() bool { return rule.Kind != "port" && rule.Family == "both" && rule.Source != "" }, Run: func() error {
			value, err := askMatchSource(view, current, rule, true)
			if err == nil {
				rule.SourceIPv6 = value
			}
			return err
		}},
		ui.PromptStep{Enabled: func() bool { return rule.Kind != "port" }, Run: func() error {
			value, err := askMatchDest(view, current, rule, false)
			if err == nil {
				rule.Dest, rule.DestIPv6 = value, ""
			}
			return err
		}},
		ui.PromptStep{Enabled: func() bool { return rule.Kind != "port" && rule.Family == "both" && rule.Dest != "" }, Run: func() error {
			value, err := askMatchDest(view, current, rule, true)
			if err == nil {
				rule.DestIPv6 = value
			}
			return err
		}},
		ui.PromptStep{Enabled: func() bool { return rule.Kind != "port" }, Run: func() error {
			sourceRulePage("填写来源网卡", rule)
			ui.PrintField("说明", "回车跳过。可使用 *，例如 br-*")
			ui.MenuExit("0/q", "返回上一步")
			value, err := askOptionalText(view, current.Iif, "来源网卡", normalizeIface)
			if err == nil {
				rule.Iif = value
			}
			return err
		}},
		ui.PromptStep{Enabled: func() bool { return rule.Kind != "port" }, Run: func() error {
			sourceRulePage("填写目标网卡", rule)
			hint := "回车跳过。可使用 *，例如 eth0"
			if activeSession == nil || !activeSession.forward {
				hint = "回车跳过。进本机的包通常没有目标网卡，这项多半匹配不到"
			}
			ui.PrintField("说明", hint)
			ui.MenuExit("0/q", "返回上一步")
			value, err := askOptionalText(view, current.Oif, "目标网卡", normalizeIface)
			if err == nil {
				rule.Oif = value
			}
			return err
		}},
		ui.PromptStep{Enabled: func() bool { return rule.Kind != "port" }, Run: func() error {
			sourceRulePage("填写来源端口", rule)
			ui.PrintField("说明", "回车跳过。输入 - 清除。例如 443 或 8000-8100")
			ui.MenuExit("0/q", "返回上一步")
			value, err := askOptionalPorts(view, current.Sport, "来源端口")
			if err == nil {
				rule.Sport = value
			}
			return err
		}},
		ui.PromptStep{Enabled: func() bool { return rule.Kind != "port" }, Run: func() error {
			sourceRulePage("填写目标端口", rule)
			ui.PrintField("说明", "回车跳过。输入 - 清除。空着表示全部目标端口")
			ui.MenuExit("0/q", "返回上一步")
			value, err := askOptionalPorts(view, current.Ports, "目标端口")
			if err == nil {
				rule.Ports = value
			}
			return err
		}},
		ui.PromptStep{Enabled: func() bool { return rule.Kind == "port" }, Run: func() error {
			sourceRulePage("填写允许访问的端口", rule)
			ui.MenuExit("0/q", "返回上一步")
			value, err := askRulePorts(view, current.Ports)
			if err == nil {
				rule.Ports = value
			}
			return err
		}},
		ui.PromptStep{Run: func() error {
			updated, err := askRuleProtocol(view, current, rule)
			if err == nil {
				rule = updated
			}
			return err
		}},
	)
	if err != nil {
		return current, err
	}
	return normalizeAllowRule(rule)
}

func askMatchSource(view *ui.UI, current, rule allowRule, second bool) (string, error) {
	family := rule.Family
	fallback := current.Source
	title := "填写来源地址"
	if family == "both" && !second {
		family = "ipv4"
	}
	if second || family == "ipv6" {
		title = "填写 IPv6 来源地址"
		family = "ipv6"
		fallback = current.SourceIPv6
		if fallback == "" && current.Source != "" && !sourcePrefix(current.Source).Addr().Is4() {
			fallback = current.Source
		}
	}
	sourceRulePage(title, rule)
	ui.PrintField("说明", "回车跳过表示不限制来源。输入 - 清除")
	ui.MenuExit("0/q", "返回上一步")
	return askOptionalSource(view, fallback, family, second)
}

func askMatchDest(view *ui.UI, current, rule allowRule, second bool) (string, error) {
	family := rule.Family
	fallback := current.Dest
	title := "填写目标地址"
	if family == "both" && !second {
		family = "ipv4"
	}
	if second || family == "ipv6" {
		title = "填写 IPv6 目标地址"
		family = "ipv6"
		fallback = current.DestIPv6
	}
	sourceRulePage(title, rule)
	ui.PrintField("说明", "回车跳过表示不限制目标。输入 - 清除")
	ui.MenuExit("0/q", "返回上一步")
	return askOptionalSource(view, fallback, family, second)
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
	ui.MenuExit("0/q", "返回上一步")
	family, err := askRuleChoice(view, "请选择 IP 版本（回车默认 "+fallback+"，0/q 返回）：", fallback,
		map[string]string{"1": "ipv4", "2": "ipv6", "3": "both", "ipv4": "ipv4", "ipv6": "ipv6", "ipv4+ipv6": "both", "both": "both"})
	if err != nil {
		return rule, err
	}
	rule.Family = family
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
	ui.MenuExit("0/q", "返回上一步")
	protocol, err := askRuleChoice(view, "请选择协议（回车默认 "+fallback+"，0/q 返回）：", fallback, map[string]string{"1": "tcp", "2": "udp", "3": "both", "tcp": "tcp", "udp": "udp", "tcp+udp": "both"})
	if err != nil {
		return rule, err
	}
	rule.Protocol = protocol
	return rule, nil
}

func changeAllowRuleDraft(view *ui.UI, rules []allowRule, id string) ([]allowRule, error) {
	current := allowRule{ID: id}
	if id == "" {
		var err error
		current.ID, err = newRuleID(rules)
		if err != nil {
			return nil, err
		}
	} else {
		found := false
		for _, rule := range rules {
			if rule.ID == id {
				current, found = rule, true
			}
		}
		if !found {
			return nil, fmt.Errorf("规则 ID 不存在，请刷新后重新选择")
		}
	}
	draft, err := askAllowRule(view, current)
	if err != nil {
		return nil, err
	}
	if draft == current {
		log.Info("规则未变化")
		return rules, nil
	}
	warnings, err := checkRuleRelations(draft, rules)
	if err != nil {
		return nil, err
	}
	sourceRulePage("检查规则", allowRule{})
	ui.PrintInfoCard("待加入草稿的规则",
		ui.CardField{Label: "规则 ID", Value: draft.ID},
		ui.CardField{Label: "IP 版本", Value: draft.familyLabel()},
		ui.CardField{Label: "规则内容", Value: draft.description()},
		ui.CardField{Label: "实际来源范围", Value: draft.sourceRange()},
	)
	if len(warnings) > 0 {
		ui.PrintWarningCard("发现覆盖或重叠",
			ui.CardField{Label: "相关规则", Value: strings.Join(warnings, "\n")},
			ui.CardField{Label: "保存方式", Value: "保留各规则 ID，不合并、不删除其他规则"},
		)
	}
	updated := append([]allowRule(nil), rules...)
	if id == "" {
		if draft.Verdict == "drop" {
			updated = append([]allowRule{draft}, updated...)
		} else {
			updated = append(updated, draft)
		}
	} else {
		for i := range updated {
			if updated[i].ID == id {
				updated[i] = draft
			}
		}
	}
	ui.PrintSuccessCard("规则已加入草稿", ui.CardField{Label: draft.ID, Value: draft.description()})
	return updated, nil
}

func queryRulesByPort(view *ui.UI, rules []allowRule) ([]allowRule, error) {
	sourceRulePage("按端口查询", allowRule{})
	ui.MenuExit("0/q", "返回删除方式选择")
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
	sourceRulePage("端口查询结果", allowRule{})
	ui.PrintInfoCard("端口查询结果",
		ui.CardField{Label: "端口 / 范围", Value: ports},
		ui.CardField{Label: "查询范围", Value: "IPv4 / IPv6、全部协议；IP 放行规则涵盖全部端口，也会列出"},
	)
	printRuleOrder(matches, "")
	return matches, nil
}

func deleteAllowRuleDraft(view *ui.UI, rules []allowRule) ([]allowRule, error) {
	mode, id := "", ""
	candidates := rules
	err := ui.RunPromptSteps(
		ui.PromptStep{Run: func() error {
			sourceRulePage("删除放行规则", allowRule{})
			ruleChoiceOption("1", "按序号 / 规则 ID 删除", "", "1")
			ui.MenuOption("2", "按端口查询后选择规则删除")
			ui.MenuExit("0/q", "返回规则配置")
			var err error
			mode, err = askRuleChoice(view, "请选择删除方式（回车默认 1）：", "1", map[string]string{"1": "id", "2": "port"})
			candidates = rules
			return err
		}},
		ui.PromptStep{Enabled: func() bool { return mode == "port" }, Run: func() error {
			var err error
			candidates, err = queryRulesByPort(view, rules)
			return err
		}},
		ui.PromptStep{Run: func() error {
			sourceRulePage("选择删除放行规则", allowRule{})
			printRuleOrder(candidates, "")
			ui.MenuExit("0/q", "返回上一步")
			var err error
			id, err = chooseRuleID(view, candidates)
			return err
		}},
	)
	if err != nil {
		return nil, err
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
	sourceRulePage("检查删除范围", allowRule{})
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
	ui.PrintSuccessCard("删除操作已加入草稿", ui.CardField{Label: "规则 ID", Value: id})
	return updated, nil
}
