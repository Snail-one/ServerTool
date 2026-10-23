package firewall

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

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

func runRuleManager(view *ui.UI) error {
	if !isRoot() {
		return fmt.Errorf("访问规则管理需要 root 权限，请使用 sudo 运行本工具")
	}
	version, err := readConfigVersion()
	if err != nil {
		return err
	}
	updated, err := editPolicyDraft(view, string(version.content), false)
	if err != nil {
		return err
	}
	if err := writeConfig(updated, version); err != nil {
		return err
	}
	if err := applyConfig([]byte(updated)); err != nil {
		return fmt.Errorf("访问规则已保存，但未成功加载；原配置已备份，可输入 r 校验加载配置重试: %w", err)
	}
	ui.PrintSuccessCard("访问规则已保存并加载")
	return nil
}

// The same draft editor is used for existing configurations and during initial
// generation. It never writes files or changes the running firewall itself.
func editPolicyDraft(view *ui.UI, original string, generating bool) (string, error) {
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
	for {
		ui.ClearScreen()
		ui.MenuTitle("防火墙管理", "访问规则管理")
		ui.PrintInfoCard("访问规则草稿（尚未保存）", accessSummaryFields(draft)...)
		ui.MenuOption("1", "公网端口")
		ui.MenuOption("2", "内网端口")
		ui.MenuOptionHint("3", "内网来源名单", "IPv4 / IPv6 地址及网段")
		ui.MenuOptionHint("4", "来源例外", "添加 / 编辑 / 删除 / 排序，优先于基础端口")
		ui.MenuOptionHint("5", "公网与内网同时设置", "批量修改所选 IP 版本和协议的端口")
		ui.MenuOption("p", "预览完整配置")
		ui.MenuOption("r", "还原本次草稿")
		if generating {
			ui.MenuShortcutHint("s", "采用草稿并返回", "返回生成配置的最终确认页")
		} else {
			ui.MenuShortcutHint("s", "保存并立即加载", "显示变更，确认后校验并备份当前配置")
		}
		ui.MenuExit("0/q", "放弃草稿并返回")
		choice, err := view.Ask("请选择：")
		if err != nil {
			return "", err
		}
		if shared.IsReturnChoice(choice) {
			return "", shared.ErrReturnToMenu
		}
		switch strings.ToLower(choice) {
		case "p":
			fmt.Println(draft)
			view.PauseWithPrompt("按回车返回草稿…")
		case "r":
			draft = original
		case "s":
			fields := policyChangeFields(original, draft)
			if len(fields) == 0 {
				if generating {
					return original, nil
				}
				log.Info("设置未变化，无需保存")
				continue
			}
			ui.PrintInfoCard("待应用的变更", fields...)
			prompt := "确认校验、备份、保存并加载整份当前配置（包括其他待加载修改）？(y/N)："
			if generating {
				prompt = "确认采用此草稿并返回最终确认页？(y/N)："
			}
			confirmed, err := view.Confirm(prompt)
			if errors.Is(err, shared.ErrReturnToMenu) || err == nil && !confirmed {
				continue
			}
			if err != nil {
				return "", err
			}
			return draft, nil
		default:
			var updated string
			switch choice {
			case "1", "2", "5":
				if err = validatePolicyConfig(draft); err != nil {
					log.Warn(err)
					view.PauseWithPrompt("按回车返回访问规则草稿…")
					continue
				}
				scope := map[string]string{"1": "1", "2": "2", "5": "3"}[choice]
				var indices []int
				indices, err = choosePolicyPortScope(view, scope)
				if err == nil {
					updated, err = changePolicyLists(view, draft, indices)
				}
			case "3":
				ui.MenuOption("1", "IPv4 内网来源名单")
				ui.MenuOption("2", "IPv6 内网来源名单")
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
			case "4":
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
			draft = updated
		}
	}
}

func choosePolicyPortScope(view *ui.UI, scope string) ([]int, error) {
	selections := []string{scope}
	for _, options := range []struct{ title, a, b string }{
		{"IP 版本", "IPv4", "IPv6"}, {"协议", "TCP", "UDP"},
	} {
		ui.MenuSection(options.title)
		ui.MenuOption("1", options.a)
		ui.MenuOption("2", options.b)
		ui.MenuOption("3", "两者同时")
		ui.MenuExit("0/q", "取消并返回草稿")
		choice, err := askRuleChoice(view, "请选择（回车默认两者同时）", "3", map[string]string{"1": "1", "2": "2", "3": "3"})
		if err != nil {
			return nil, err
		}
		selections = append(selections, choice)
	}
	var indices []int
	for i := 0; i < 8; i++ {
		if (selections[0] == "3" || (selections[0] == "1") == (i < 4)) &&
			(selections[1] == "3" || (selections[1] == "1") == (i%4 < 2)) &&
			(selections[2] == "3" || (selections[2] == "1") == (i%2 == 0)) {
			indices = append(indices, i)
		}
	}
	return indices, nil
}

func changePolicyLists(view *ui.UI, content string, indices []int) (string, error) {
	list := policyLists[indices[0]]
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
	ui.PrintInfoCard("编辑所选列表", fields...)
	ui.MenuOption("1", "添加")
	if list.family == 0 {
		ui.MenuOptionHint("2", "删除端口 / 范围", "从业务列表扣除，SSH 端口始终保留")
		ui.MenuOption("3", "替换全部业务端口")
		ui.MenuOptionHint("4", "清空业务端口", "只保留 SSH 端口")
	} else {
		ui.MenuOptionHint("2", "删除名单条目", "精确匹配规范化后的地址 / CIDR")
		ui.MenuOption("3", "替换整个名单")
		ui.MenuOption("4", "恢复默认内网名单")
		ui.MenuOption("5", "按序号编辑单个条目")
		ui.PrintField("名单说明", "支持单个 IP 和 CIDR，逗号分隔；自动规范化并合并重复及被包含条目；至少保留一项")
	}
	ui.MenuExit("0/q", "取消并返回草稿")
	choices := map[string]string{"1": "add", "2": "remove", "3": "replace", "4": "reset"}
	if list.family != 0 {
		choices["5"] = "edit"
	}
	operation, err := askRuleChoice(view, "请选择操作", "", choices)
	if err != nil {
		return "", err
	}
	current, _ := readPolicyValue(content, list)
	editIndex := -1
	if operation == "edit" {
		entries := strings.Split(current, ", ")
		for i, entry := range entries {
			ui.MenuOption(strconv.Itoa(i+1), entry)
		}
		for {
			value, err := view.Ask("请输入要编辑的条目序号（0/q 返回）：")
			if err != nil {
				return "", err
			}
			if shared.IsReturnChoice(value) {
				return "", shared.ErrReturnToMenu
			}
			number, err := strconv.Atoi(value)
			if err == nil && number >= 1 && number <= len(entries) {
				editIndex = number - 1
				break
			}
			ui.InvalidChoice()
		}
	}
	for {
		value := ""
		if operation != "reset" {
			prompt := "请输入端口或范围，多个用逗号分隔，例如 80,443,8000-8100（0/q 返回）："
			if list.family != 0 {
				prompt = fmt.Sprintf("请输入 IPv%d 地址 / CIDR，多个用逗号分隔（0/q 返回）：", list.family)
			}
			value, err = view.Ask(prompt)
			if err != nil {
				return "", err
			}
			if shared.IsReturnChoice(value) {
				return "", shared.ErrReturnToMenu
			}
			if value == "" {
				log.Warn("输入不能为空；清空业务端口请选择 4")
				continue
			}
		}
		updated := content
		for _, index := range indices {
			selected := policyLists[index]
			current, err = readPolicyValue(updated, selected)
			if err == nil {
				var next string
				next, err = changePolicyValue(selected, current, operation, value, editIndex)
				if err == nil {
					updated, err = withPolicyValue(updated, selected, next)
				}
			}
			if err != nil {
				break
			}
		}
		if err == nil {
			return updated, nil
		}
		log.Warn(err)
		if operation == "reset" {
			return "", err
		}
	}
}

func changePolicyValue(list policyList, current, operation, value string, editIndex int) (string, error) {
	if operation == "reset" {
		return list.defaults, nil
	}
	if list.family == 0 {
		old, err := parsePolicyPorts(current)
		if err != nil {
			return "", err
		}
		ports, err := parsePolicyPorts(value)
		if err != nil {
			return "", err
		}
		switch operation {
		case "add":
			ports = unionPorts(append(old, ports...))
		case "remove":
			ports = subtractPorts(old, ports)
		}
		return policyPortsText(ports), nil
	}
	entries, err := parsePolicySources(value, list.family)
	if err != nil {
		return "", err
	}
	old := strings.Split(current, ", ")
	switch operation {
	case "add":
		entries = append(old, entries...)
	case "remove":
		removed := make(map[string]bool)
		for _, entry := range entries {
			removed[entry] = true
		}
		entries = nil
		for _, entry := range old {
			if !removed[entry] {
				entries = append(entries, entry)
			}
		}
		if len(entries) == 0 {
			return "", fmt.Errorf("内网名单至少保留一项；可替换整个名单或恢复默认")
		}
	case "edit":
		if len(entries) != 1 || editIndex < 0 || editIndex >= len(old) {
			return "", fmt.Errorf("编辑单个条目时请只输入一个地址或 CIDR")
		}
		old[editIndex] = entries[0]
		entries = old
	}
	entries, err = parsePolicySources(strings.Join(entries, ", "), list.family)
	return strings.Join(entries, ", "), err
}
