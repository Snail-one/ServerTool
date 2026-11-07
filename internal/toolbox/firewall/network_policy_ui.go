package firewall

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"snail_tool/internal/input"
	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

func policyFields(content string) []ui.CardField {
	var fields []ui.CardField
	for _, list := range policyLists {
		value, err := readPolicyValue(content, list)
		if err != nil {
			value = err.Error()
		} else if list.family == 0 {
			if value == "" {
				value = "仅 SSH 端口"
			} else {
				value = "SSH 端口 + " + value
			}
		}
		fields = append(fields, ui.CardField{Label: list.label, Value: value})
	}
	return fields
}

func policyChangeFields(before, after string) []ui.CardField {
	var fields []ui.CardField
	old, next := policyFields(before), policyFields(after)
	for i := range old {
		if old[i].Value != next[i].Value {
			fields = append(fields, ui.CardField{Label: next[i].Label, Value: next[i].Value, Detail: "原设置：" + old[i].Value})
		}
	}
	return append(fields, ruleChangeFields(before, after)...)
}

func runAccessRuleManager(view *ui.UI) error {
	if !isRoot() {
		return fmt.Errorf("访问规则管理需要 root 权限，请使用 sudo 运行本工具")
	}
	version, err := readConfigVersion()
	if err != nil {
		return err
	}
	updated, err := editAccessRulesDraft(view, string(version.content), false)
	if err != nil {
		return err
	}
	if err := saveAccessRules(version, updated); err != nil {
		return err
	}
	ui.PrintSuccessCard("访问规则已保存并加载")
	return nil
}

// The same draft editor is used for existing configurations and during initial
// generation. It never writes files or changes the running firewall itself.
func editAccessRulesDraft(view *ui.UI, original string, generating bool) (string, error) {
	if !hasManagedConfig(original) {
		return "", fmt.Errorf("请先选择 6 生成本工具防火墙配置")
	}
	if _, _, _, err := inputChainBounds(original); err != nil {
		return "", err
	}
	if _, err := managedRules(original); err != nil {
		return "", err
	}
	draft := original
	notice := ""
	for {
		ui.ClearScreen()
		ui.MenuTitle("防火墙管理", "访问规则管理")
		ui.PrintInfoCard("访问规则草稿（尚未保存）", accessSummaryFields(draft)...)
		ui.MenuOptionHint("1", "端口放行", "同时设置公网和内网端口")
		ui.MenuOptionHint("2", "公网端口", "单独设置公网开放端口")
		ui.MenuOptionHint("3", "内网端口", "单独设置内网开放端口")
		ui.MenuOptionHint("4", "内网 IP 范围", "设置哪些 IP / 网段按内网处理")
		ui.MenuOptionHint("5", "来源 IP 放行", "为指定 IP / 网段额外放行端口")
		fmt.Println()
		ui.MenuOptionHint("p", "预览完整配置", "查看本次修改后的完整配置")
		ui.MenuOptionHint("r", "还原本次草稿", "撤销本次全部修改")
		if generating {
			ui.MenuShortcutHint("s", "采用草稿并返回", "进入最终确认，暂不写入配置")
		} else {
			ui.MenuShortcutHint("s", "保存并立即加载", "确认后备份并应用全部修改")
		}
		fmt.Println()
		ui.MenuExit("0/q", "放弃草稿并返回")
		if notice != "" {
			fmt.Println()
			ui.PrintSectionCard("提示", ui.CardField{Value: notice})
			fmt.Println()
			notice = ""
		}
		choice, err := view.Ask("请选择：")
		if err != nil {
			return "", err
		}
		if shared.IsReturnChoice(choice) {
			return "", shared.ErrReturnToMenu
		}
		switch strings.ToLower(choice) {
		case "p":
			ui.ClearScreen()
			ui.MenuTitle("防火墙管理", "访问规则管理", "预览配置")
			fmt.Println(draft)
			view.PauseWithPrompt("按回车返回草稿…")
		case "r":
			draft = original
			notice = "已还原本次全部修改"
		case "s":
			fields := policyChangeFields(original, draft)
			if len(fields) == 0 {
				if generating {
					return original, nil
				}
				notice = "设置未变化，无需保存"
				continue
			}
			ui.ClearScreen()
			ui.MenuTitle("防火墙管理", "访问规则管理", "确认变更")
			ui.PrintInfoCard("待应用的变更", fields...)
			prompt := "确认校验、备份、保存并加载整份当前配置（包括其他待加载修改）？(y/N，0/q 返回)："
			if generating {
				prompt = "确认采用此草稿并返回最终确认页？(y/N，0/q 返回)："
			}
			confirmed, err := view.Confirm(prompt)
			if errors.Is(err, shared.ErrReturnToMenu) || err == nil && !confirmed {
				notice = "尚未保存，草稿已保留，可继续编辑或返回放弃"
				continue
			}
			if err != nil {
				return "", err
			}
			return draft, nil
		default:
			var updated string
			switch choice {
			case "1", "2", "3":
				if err = validatePolicyConfig(draft); err != nil {
					log.Warn(err)
					view.PauseWithPrompt("按回车返回访问规则草稿…")
					continue
				}
				scope := map[string]string{"1": "3", "2": "1", "3": "2"}[choice]
				_, err = promptPolicyPortScope(view, scope, func(indices []int) error {
					var editErr error
					updated, editErr = changePolicyLists(view, draft, indices)
					return editErr
				})
			case "4":
				for {
					ui.ClearScreen()
					ui.MenuTitle("防火墙管理", "访问规则管理", "内网 IP 范围")
					ui.PrintInfoCard("按来源地址划分内网",
						ui.CardField{Label: "范围内的来源", Value: "使用内网端口策略"},
						ui.CardField{Label: "其他来源", Value: "使用公网端口策略"},
					)
					ui.MenuOption("1", "IPv4 内网范围")
					ui.MenuOption("2", "IPv6 内网范围")
					ui.MenuExit("0/q", "返回访问规则草稿")
					var family string
					family, err = askRuleChoice(view, "请选择 IP 版本", "", map[string]string{"1": "ipv4", "2": "ipv6"})
					if err == nil {
						index := 8
						if family == "ipv6" {
							index = 9
						}
						updated, err = changePolicyLists(view, draft, []int{index})
					}
					if errors.Is(err, shared.ErrReturnToMenu) && family != "" {
						continue
					}
					break
				}
			case "5":
				updated, err = editSourceRulesDraft(view, draft)
			default:
				ui.InvalidChoice()
				continue
			}
			if errors.Is(err, shared.ErrReturnToMenu) {
				continue
			}
			if err != nil {
				log.Warn(err)
				view.PauseWithPrompt("按回车返回访问规则草稿…")
				continue
			}
			if updated != draft {
				notice = "已更新草稿；输入 s 统一保存并加载，输入 r 还原全部修改"
			}
			draft = updated
		}
	}
}

func choosePolicyPortScope(view *ui.UI, scope string) ([]int, error) {
	return promptPolicyPortScope(view, scope, nil)
}

func promptPolicyPortScope(view *ui.UI, scope string, edit func([]int) error) ([]int, error) {
	selections := []string{scope, "3", "3"}
	var indices []int
	updateIndices := func() {
		indices = nil
		for i := 0; i < 8; i++ {
			if (selections[0] == "3" || (selections[0] == "1") == (i < 4)) &&
				(selections[1] == "3" || (selections[1] == "1") == (i%4 < 2)) &&
				(selections[2] == "3" || (selections[2] == "1") == (i%2 == 0)) {
				indices = append(indices, i)
			}
		}
	}
	var steps []ui.PromptStep
	for index, option := range []struct{ title, a, b, both string }{
		{"选择 IP 版本", "IPv4", "IPv6", "IPv4 + IPv6"},
		{"选择协议", "TCP", "UDP", "TCP + UDP"},
	} {
		index, option := index, option
		steps = append(steps, ui.PromptStep{Run: func() error {
			ui.ClearScreen()
			ui.MenuTitle("防火墙管理", "访问规则管理", policyScopeTitle(scope), option.title)
			ui.PrintInfoCard(policyScopeTitle(scope), ui.CardField{Label: "当前步骤", Value: fmt.Sprintf("%d / 3 · %s", index+1, option.title)})
			for i, label := range []string{option.a, option.b, option.both} {
				ruleChoiceOption(strconv.Itoa(i+1), label, "", selections[index+1])
			}
			if index > 0 {
				ui.MenuOption("b", "上一步：选择 IP 版本")
			}
			ui.MenuExit("0/q", "返回上一步")
			choice, err := askRuleChoice(view, "请选择", selections[index+1], map[string]string{"1": "1", "2": "2", "3": "3", "b": "back"})
			if err != nil {
				return err
			}
			if choice == "back" {
				return shared.ErrReturnToMenu
			}
			selections[index+1] = choice
			updateIndices()
			return nil
		}})
	}
	if edit != nil {
		steps = append(steps, ui.PromptStep{Run: func() error { return edit(indices) }})
	}
	err := ui.RunPromptSteps(steps...)
	return indices, err
}

func policyScopeTitle(scope string) string {
	return map[string]string{"1": "公网端口", "2": "内网端口", "3": "公网与内网端口"}[scope]
}

func changePolicyLists(view *ui.UI, content string, indices []int) (string, error) {
	list := policyLists[indices[0]]
	title := list.label
	if list.family == 0 {
		scope := "1"
		if indices[0] >= 4 {
			scope = "2"
		} else if indices[len(indices)-1] >= 4 {
			scope = "3"
		}
		title = policyScopeTitle(scope)
	}
	var fields []ui.CardField
	for _, index := range indices {
		value, err := readPolicyValue(content, policyLists[index])
		if err != nil {
			return "", err
		}
		if value == "" {
			value = "无附加业务端口，仅 SSH 端口"
		}
		fields = append(fields, ui.CardField{Label: policyLists[index].label, Value: value})
	}
	if list.family == 0 {
		allEqual := true
		for _, field := range fields[1:] {
			allEqual = allEqual && field.Value == fields[0].Value
		}
		if allEqual {
			families, protocols := "IPv4", "TCP"
			var ipv4, ipv6, tcp, udp bool
			for _, index := range indices {
				ipv4 = ipv4 || index%4 < 2
				ipv6 = ipv6 || index%4 >= 2
				tcp = tcp || index%2 == 0
				udp = udp || index%2 == 1
			}
			if ipv4 && ipv6 {
				families = "IPv4 + IPv6"
			} else if ipv6 {
				families = "IPv6"
			}
			if tcp && udp {
				protocols = "TCP + UDP"
			} else if udp {
				protocols = "UDP"
			}
			fields = []ui.CardField{
				{Label: "编辑范围", Value: families + " · " + protocols},
				{Label: "当前业务端口", Value: fields[0].Value},
			}
		}
		fields = append([]ui.CardField{{Label: "当前步骤", Value: "3 / 3 · 选择操作"}}, fields...)
	}
	operation, value, editIndex := "", "", -1
	err := ui.RunPromptSteps(
		ui.PromptStep{Run: func() error {
			ui.ClearScreen()
			ui.MenuTitle("防火墙管理", "访问规则管理", title, "选择操作")
			ui.PrintInfoCard("编辑所选列表", fields...)
			ui.MenuOption("1", "添加")
			if list.family == 0 {
				ui.MenuOptionHint("2", "删除端口 / 范围", "从业务列表扣除，SSH 端口始终保留")
				ui.MenuOption("3", "替换全部业务端口")
				ui.MenuOptionHint("4", "清空业务端口", "只保留 SSH 端口")
			} else {
				ui.MenuOptionHint("2", "删除地址 / 网段", "精确匹配规范化后的地址 / CIDR")
				ui.MenuOption("3", "替换整个范围")
				ui.MenuOption("4", "恢复默认内网 IP 范围")
				ui.MenuOption("5", "按序号编辑单个条目")
				ui.PrintField("范围说明", "支持单个 IP 和 CIDR，逗号分隔；自动规范化并合并重复及被包含条目；至少保留一项")
			}
			ui.MenuExit("0/q", "返回上一步")
			choices := map[string]string{"1": "add", "2": "remove", "3": "replace", "4": "reset"}
			if list.family != 0 {
				choices["5"] = "edit"
			}
			var err error
			operation, err = askRuleChoice(view, "请选择操作", "", choices)
			editIndex = -1
			return err
		}},
		ui.PromptStep{Enabled: func() bool { return operation == "edit" }, Run: func() error {
			current, _ := readPolicyValue(content, list)
			entries := strings.Split(current, ", ")
			ui.ClearScreen()
			ui.MenuTitle("防火墙管理", "访问规则管理", title, "选择编辑条目")
			for i, entry := range entries {
				ui.MenuOption(strconv.Itoa(i+1), entry)
			}
			ui.MenuExit("0/q", "返回操作选择")
			for {
				raw, err := view.Ask("请输入要编辑的条目序号（0/q 返回）：")
				if err != nil {
					return err
				}
				if shared.IsReturnChoice(raw) {
					return shared.ErrReturnToMenu
				}
				number, err := input.Number(raw, 1, len(entries))
				if err == nil && number >= 1 && number <= len(entries) {
					editIndex = number - 1
					return nil
				}
				ui.InvalidChoice()
			}
		}},
		ui.PromptStep{Enabled: func() bool { return operation != "reset" }, Run: func() error {
			ui.ClearScreen()
			action := map[string]string{"add": "添加", "remove": "删除", "replace": "替换", "edit": "编辑条目"}[operation]
			ui.MenuTitle("防火墙管理", "访问规则管理", title, action)
			inputFields := append([]ui.CardField(nil), fields...)
			if list.family == 0 {
				inputFields[0].Value = "3 / 3 · " + action + "端口"
			}
			if editIndex >= 0 {
				current, _ := readPolicyValue(content, list)
				inputFields = append(inputFields, ui.CardField{Label: "编辑条目", Value: strings.Split(current, ", ")[editIndex], Detail: "回车保留当前条目"})
			}
			ui.PrintInfoCard("编辑内容", inputFields...)
			ui.MenuExit("0/q", "返回上一步")
			for {
				prompt := "请输入端口或范围，多个用逗号分隔，例如 80,443,8000-8100（0/q 返回）："
				if list.family != 0 {
					prompt = fmt.Sprintf("请输入 IPv%d 地址 / CIDR，多个用逗号分隔（0/q 返回）：", list.family)
				}
				if operation == "edit" {
					prompt = "请输入新的地址 / CIDR（回车保留当前条目，0/q 返回）："
				}
				raw, err := view.Ask(prompt)
				if err != nil {
					return err
				}
				if shared.IsReturnChoice(raw) {
					return shared.ErrReturnToMenu
				}
				if raw == "" {
					if operation == "edit" {
						current, _ := readPolicyValue(content, list)
						raw = strings.Split(current, ", ")[editIndex]
					} else {
						if list.family == 0 {
							log.Warn("输入不能为空；清空业务端口请返回后选择 4")
						} else {
							log.Warn("输入不能为空，请填写至少一个地址或 CIDR")
						}
						continue
					}
				}
				if _, err := withPolicyChanges(content, indices, operation, raw, editIndex); err != nil {
					log.Warn(err)
					continue
				}
				value = raw
				return nil
			}
		}},
	)
	if err != nil {
		return "", err
	}
	return withPolicyChanges(content, indices, operation, value, editIndex)
}
