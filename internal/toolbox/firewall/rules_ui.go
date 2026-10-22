package firewall

import (
	"fmt"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

func runRuleManager(view *ui.UI) error {
	if !isRoot() {
		return fmt.Errorf("规则管理需要 root 权限，请使用 sudo 运行本工具")
	}
	for {
		version, err := readConfigVersion()
		if err != nil {
			return err
		}
		if !version.exists {
			return fmt.Errorf("请先选择 6 生成防火墙配置，再管理放行规则")
		}
		rules, err := managedRules(string(version.content))
		if err != nil {
			return err
		}
		ui.ClearScreen()
		ui.MenuTitle("防火墙管理", "放行规则管理")
		ui.PrintInfoCard("来源入站放行",
			ui.CardField{Label: "规则数量", Value: fmt.Sprintf("%d 条", len(rules))},
			ui.CardField{Label: "管理范围", Value: "本页创建的附加放行规则；基础端口和手写规则见完整配置"},
			ui.CardField{Label: "生效方式", Value: "确认后校验、备份、保存并立即加载"},
		)
		printAllowRules(rules)
		ui.MenuOption("1", "添加放行规则")
		ui.MenuOption("2", "按规则 ID 编辑")
		ui.MenuOption("3", "删除规则")
		ui.MenuOption("4", "按端口 / 范围查询")
		ui.MenuOption("r", "刷新规则")
		ui.MenuExit("0/q", "返回防火墙管理")
		choice, err := view.Ask("请选择：")
		if err != nil {
			return err
		}
		if shared.IsReturnChoice(choice) {
			return shared.ErrReturnToMenu
		}
		switch strings.ToLower(choice) {
		case "1":
			shared.RunAction(view, "添加规则失败，已返回规则管理", func() error {
				return changeAllowRule(view, version, rules, "")
			})
		case "2":
			shared.RunAction(view, "编辑规则失败，已返回规则管理", func() error {
				ui.ClearScreen()
				ui.MenuTitle("防火墙管理", "选择编辑规则")
				printAllowRules(rules)
				id, err := chooseRuleID(view, rules)
				if err != nil {
					return err
				}
				return changeAllowRule(view, version, rules, id)
			})
		case "3":
			shared.RunAction(view, "删除规则失败，已返回规则管理", func() error {
				return deleteAllowRule(view, version, rules)
			})
		case "4":
			shared.RunAction(view, "查询规则失败，已返回规则管理", func() error {
				_, err := queryRulesByPort(view, rules)
				return err
			})
		case "r":
			continue
		default:
			ui.InvalidChoice()
			view.Pause()
		}
	}
}

func printAllowRules(rules []allowRule) {
	fmt.Println()
	if len(rules) == 0 {
		fmt.Println("没有匹配的附加放行规则")
	} else {
		for _, rule := range rules {
			ui.PrintField(rule.ID, rule.description())
		}
	}
	fmt.Println()
}

func chooseRuleID(view *ui.UI, rules []allowRule) (string, error) {
	if len(rules) == 0 {
		return "", fmt.Errorf("没有可选择的规则")
	}
	for {
		id, err := view.Ask("请输入列表中的完整规则 ID（0/q 返回）：")
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
		fmt.Println("未找到该规则 ID，请从当前列表选择")
	}
}

func askRuleChoice(view *ui.UI, prompt, fallback string, choices map[string]string) (string, error) {
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

func askRuleSource(view *ui.UI, current string) (string, error) {
	for {
		prompt := "请输入来源 IP / CIDR（0/q 返回）："
		if current != "" {
			prompt = "请输入来源 IP / CIDR（回车保留 " + current + "，0/q 返回）："
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
		ui.PrintInfoCard("实际来源匹配范围",
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
	rule := allowRule{ID: current.ID}
	ui.MenuSection("请选择入站放行类型")
	ui.MenuOptionHint("1", "端口放行", "任意来源 IPv4 / IPv6")
	ui.MenuOptionHint("2", "来源 IP 放行", "指定来源的全部 TCP / UDP 端口")
	ui.MenuOptionHint("3", "来源 IP + 端口", "指定来源和端口 / 范围")
	ui.MenuExit("0/q", "返回")
	fallback := map[string]string{"port": "1", "source": "2", "source_port": "3"}[current.Kind]
	if fallback == "" {
		fallback = "1"
	}
	kind, err := askRuleChoice(view, "请选择类型（回车默认 "+fallback+"）：", fallback, map[string]string{"1": "port", "2": "source", "3": "source_port"})
	if err != nil {
		return rule, err
	}
	rule.Kind = kind
	if kind != "port" {
		rule.Source, err = askRuleSource(view, current.Source)
		if err != nil {
			return rule, err
		}
	}
	if kind != "source" {
		rule.Ports, err = askRulePorts(view, current.Ports)
		if err != nil {
			return rule, err
		}
	}
	ui.MenuSection("请选择放行协议")
	ui.MenuOption("1", "TCP")
	ui.MenuOption("2", "UDP")
	ui.MenuOption("3", "TCP + UDP")
	fallback = map[string]string{"tcp": "1", "udp": "2", "both": "3"}[current.Protocol]
	if fallback == "" {
		fallback = "1"
	}
	rule.Protocol, err = askRuleChoice(view, "请选择协议（回车默认 "+fallback+"，0/q 返回）：", fallback, map[string]string{"1": "tcp", "2": "udp", "3": "both", "tcp": "tcp", "udp": "udp", "tcp+udp": "both"})
	if err != nil {
		return rule, err
	}
	return normalizeAllowRule(rule)
}

func changeAllowRule(view *ui.UI, version configVersion, rules []allowRule, id string) error {
	ui.ClearScreen()
	if id == "" {
		ui.MenuTitle("防火墙管理", "添加放行规则")
	} else {
		ui.MenuTitle("防火墙管理", "编辑放行规则")
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
	draft, err := askAllowRule(view, current)
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
	ui.PrintInfoCard("即将保存的入站放行规则",
		ui.CardField{Label: "规则 ID", Value: draft.ID},
		ui.CardField{Label: "放行内容", Value: draft.description()},
		ui.CardField{Label: "实际来源范围", Value: sourceRange(draft.Source)},
	)
	if len(warnings) > 0 {
		ui.PrintWarningCard("发现覆盖或重叠",
			ui.CardField{Label: "相关规则", Value: strings.Join(warnings, "\n")},
			ui.CardField{Label: "保存方式", Value: "保留各规则 ID，不合并、不删除其他规则"},
		)
	}
	confirmed, err := view.Confirm("确认保存此规则并立即加载？(y/N)：")
	if err != nil {
		return err
	}
	if !confirmed {
		return shared.ErrReturnToMenu
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
	if err := saveManagedRules(version, updated); err != nil {
		return err
	}
	ui.PrintSuccessCard("放行规则已保存并加载", ui.CardField{Label: draft.ID, Value: draft.description()})
	return nil
}

func queryRulesByPort(view *ui.UI, rules []allowRule) ([]allowRule, error) {
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "查询放行规则")
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
	ui.PrintInfoCard("端口查询结果",
		ui.CardField{Label: "端口 / 范围", Value: ports},
		ui.CardField{Label: "查询范围", Value: "全部协议；IP 放行规则涵盖全部端口，也会列出"},
	)
	printAllowRules(matches)
	return matches, nil
}

func deleteAllowRule(view *ui.UI, version configVersion, rules []allowRule) error {
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "删除放行规则")
	ui.MenuOption("1", "按规则 ID 删除")
	ui.MenuOption("2", "按端口查询后选择规则删除")
	ui.MenuExit("0/q", "返回")
	mode, err := askRuleChoice(view, "请选择删除方式：", "1", map[string]string{"1": "id", "2": "port"})
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
		printAllowRules(candidates)
	}
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
	ui.PrintWarningCard("即将删除一条放行规则",
		ui.CardField{Label: "规则 ID", Value: target.ID},
		ui.CardField{Label: "放行内容", Value: target.description()},
		ui.CardField{Label: "实际来源范围", Value: sourceRange(target.Source)},
		ui.CardField{Label: "删除范围", Value: "仅删除此 ID；其他放行规则及基础端口保持，仍可能允许相同流量"},
	)
	for _, rule := range updated {
		if target.overlaps(rule) {
			ui.PrintField("仍保留的重叠规则 "+rule.ID, rule.description())
		}
	}
	confirmed, err := view.Confirm("确认仅删除规则 " + id + " 并立即加载？(y/N)：")
	if err != nil {
		return err
	}
	if !confirmed {
		return shared.ErrReturnToMenu
	}
	if err := saveManagedRules(version, updated); err != nil {
		return err
	}
	ui.PrintSuccessCard("指定规则已删除并加载", ui.CardField{Label: "规则 ID", Value: id})
	return nil
}
