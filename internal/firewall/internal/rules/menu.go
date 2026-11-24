package rules

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/input"
	"snail_tool/internal/log"
	"snail_tool/internal/ui"
)

func (session Session) PolicyFields(content string) []ui.CardField {
	var fields []ui.CardField
	for _, list := range session.lists {
		value, err := ReadPolicyValue(content, list)
		if err != nil {
			value = err.Error()
		} else if list.Family == 0 {
			if list.KeepSSH {
				if value == "" {
					value = "仅 SSH 端口"
				} else {
					value = "SSH 端口 + " + value
				}
			} else if value == "" {
				value = "无"
			}
		}
		fields = append(fields, ui.CardField{Label: list.Label, Value: value})
	}
	return fields
}

func (session Session) PolicyChangeFields(before, after string) []ui.CardField {
	if before == after {
		return nil
	}
	var fields []ui.CardField
	old, next := session.PolicyFields(before), session.PolicyFields(after)
	for i := range old {
		if old[i].Value != next[i].Value {
			fields = append(fields, ui.CardField{Label: next[i].Label, Value: next[i].Value, Detail: "原设置：" + old[i].Value, Changed: true})
		}
	}
	return append(fields, session.ruleChangeFields(before, after)...)
}

func (session Session) Run(view *ui.UI) error {
	if !session.options.IsRoot() {
		return fmt.Errorf("%s管理需要 root 权限，请使用 sudo 运行本工具", session.label)
	}
	for {
		edit, err := session.openDraft()
		if err != nil {
			return err
		}
		saved, err := edit(view)
		if err != nil {
			return err
		}
		if saved {
			ui.ClearScreen()
			ui.MenuTitle("防火墙管理", session.title)
			ui.PrintSuccessCard(session.label + "已保存并加载")
			view.PauseWithPrompt("按回车返回" + session.title + "…")
		}
	}
}

func (session Session) openDraft() (func(*ui.UI) (bool, error), error) {
	version, err := session.options.Store.Read()
	if err != nil {
		return nil, err
	}
	original := string(version.Content)
	return func(view *ui.UI) (bool, error) {
		draft, err := session.EditPolicyDraft(view, original, false, func(draft string) (bool, error) {
			return session.SaveRules(version, draft)
		})
		return err == nil && draft != original, err
	}, nil
}

// save reports whether the draft was written, even if loading it failed.
// When unset, the editor returns the draft to the caller for further review.
func (session Session) EditPolicyDraft(view *ui.UI, original string, generating bool, save func(string) (bool, error)) (string, error) {
	forwarding := session.forward
	showDocker := forwarding && !generating && save != nil
	var forwardConfigErr error
	if forwarding {
		forwardConfigErr = session.validateForwardConfig(original)
		if forwardConfigErr != nil && !showDocker {
			return "", forwardConfigErr
		}
	} else {
		if !session.options.HasManagedConfig(original) {
			return "", fmt.Errorf("请先选择 d 生成本工具防火墙配置")
		}
		if _, _, _, err := configuration.InputChainBounds(original); err != nil {
			return "", err
		}
		if _, err := ManagedRules(original); err != nil {
			return "", err
		}
	}
	draft := original
	notice := ""
	for {
		changes := session.PolicyChangeFields(original, draft)
		ui.ClearScreen()
		ui.MenuTitle("防火墙管理", session.title)
		var summary []ui.CardField
		label := "访问规则"
		if forwarding {
			label = "转发规则"
			if forwardConfigErr != nil {
				summary = []ui.CardField{{Label: "配置状态", Value: forwardConfigErr.Error()}}
			} else {
				summary = session.summary(draft)
			}
		} else {
			summary = session.summary(draft)
		}
		card := label + "草稿（尚未保存）"
		if !generating && len(changes) == 0 {
			card = label + "（当前配置）"
		}
		ui.PrintInfoCard(card, summary...)
		var dockerFields []ui.CardField
		if showDocker {
			dockerFields = session.immediate.Fields(original)
			ui.PrintSectionCard("Docker 转发状态", dockerFields...)
		}
		if notice != "" {
			fmt.Println()
			ui.PrintSectionCard("提示", ui.CardField{Value: notice})
			fmt.Println()
			notice = ""
		}
		if forwardConfigErr == nil {
			ui.MenuOptionHint("1", "端口放行", "同时设置公网和内网端口")
			ui.MenuOptionHint("2", "公网端口", "单独设置公网开放端口")
			ui.MenuOptionHint("3", "内网端口", "单独设置内网开放端口")
			ui.MenuOptionHint("5", "规则配置", "来源、目标、网卡和端口；接受或拒绝")
		}
		if showDocker {
			if forwardConfigErr == nil {
				fmt.Println()
			}
			session.immediate.Print(dockerFields)
		}
		if forwardConfigErr == nil {
			fmt.Println()
			ui.MenuShortcutHint("l", "内网 IP 范围", "设置哪些 IP / 网段按内网处理")
			ui.MenuShortcutHint("p", "预览配置", "查看本次变更及完整配置")
		}
		if len(changes) > 0 {
			ui.MenuShortcutHint("r", "还原本次草稿", "撤销本次全部修改")
		}
		if generating {
			ui.MenuShortcutHint("s", "采用草稿并返回", "进入最终确认，暂不写入配置")
		} else if len(changes) > 0 {
			ui.MenuShortcutHint("s", "保存并立即生效", "校验、备份并应用全部修改")
		}
		exitLabel := "返回防火墙管理"
		if len(changes) > 0 {
			exitLabel = "放弃草稿并返回"
		} else if generating {
			exitLabel = "返回配置确认"
		}
		ui.MenuExit("0/q", exitLabel)
		if len(changes) > 0 {
			warning := "⚠ 有未保存的修改。输入 s 保存并应用。"
			if generating {
				warning = "⚠ 有未保存的修改。输入 s 采用草稿，返回配置确认。"
			}
			fmt.Println(ui.WarningBoldText(warning))
			fmt.Println()
		}
		choice, err := view.Ask("请选择：")
		if err != nil {
			return "", err
		}
		if ui.IsReturnChoice(choice) {
			if len(changes) == 0 {
				return "", ui.ErrReturnToMenu
			}
			discard, err := view.Confirm("还有未保存的配置，是否放弃修改并返回？(y/N)：")
			if errors.Is(err, ui.ErrReturnToMenu) || err == nil && !discard {
				notice = "草稿已保留"
				continue
			}
			if err != nil {
				return "", err
			}
			return "", ui.ErrReturnToMenu
		}
		choice = strings.ToLower(choice)
		if showDocker {
			action, failure := session.immediate.Choose(view, choice)
			if action != nil {
				if len(changes) > 0 {
					notice = "Docker 设置会立即应用，请先保存或还原转发规则草稿"
					continue
				}
				ui.RunAction(view, failure, action)
				// Reopen the editor with a fresh snapshot after an immediate update.
				return original, nil
			}
		}
		if forwardConfigErr != nil {
			ui.InvalidChoice()
			continue
		}
		switch choice {
		case "p":
			ui.ClearScreen()
			ui.MenuTitle("防火墙管理", session.title, "预览配置")
			if len(changes) > 0 {
				ui.PrintInfoCard("待应用的变更", changes...)
			}
			fmt.Println(draft)
			view.PauseWithPrompt("按回车返回" + session.title + "…")
		case "r":
			draft = original
			notice = "已还原本次全部修改"
		case "s":
			if len(changes) == 0 {
				if generating {
					return original, nil
				}
				notice = "设置未变化，无需保存"
				continue
			}
			if !generating {
				if save != nil {
					saved, err := save(draft)
					if err != nil && !saved {
						notice = "保存失败，草稿已保留：" + err.Error()
						continue
					}
					if err != nil {
						return "", err
					}
				}
				return draft, nil
			}
			ui.ClearScreen()
			ui.MenuTitle("防火墙管理", session.title, "确认变更")
			ui.PrintInfoCard("待应用的变更", changes...)
			prompt := "确认采用此草稿并返回最终确认页？(y/N，0/q 返回)："
			confirmed, err := view.Confirm(prompt)
			if errors.Is(err, ui.ErrReturnToMenu) || err == nil && !confirmed {
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
				if forwarding {
					err = session.validateForwardConfig(draft)
				} else {
					err = session.ValidateConfig(draft)
				}
				if err != nil {
					log.Warn(err)
					view.PauseWithPrompt("按回车返回" + session.title + "…")
					continue
				}
				scope := map[string]string{"1": "3", "2": "1", "3": "2"}[choice]
				_, err = session.PromptPolicyPortScope(view, scope, func(indices []int) error {
					var editErr error
					updated, editErr = session.changePolicyLists(view, draft, indices)
					return editErr
				})
			case "l":
				for {
					ui.ClearScreen()
					ui.MenuTitle("防火墙管理", session.title, "内网 IP 范围")
					ui.PrintInfoCard("按来源地址划分内网",
						ui.CardField{Label: "范围内的来源", Value: "使用内网端口策略"},
						ui.CardField{Label: "其他来源", Value: "使用公网端口策略"},
					)
					ui.MenuOption("1", "IPv4 内网范围")
					ui.MenuOption("2", "IPv6 内网范围")
					ui.MenuExit("0/q", "返回"+session.title)
					var family string
					family, err = ui.AskChoice(view, "请选择 IP 版本", "", map[string]string{"1": "ipv4", "2": "ipv6"})
					if err == nil {
						index := 8
						if family == "ipv6" {
							index = 9
						}
						updated, err = session.changePolicyLists(view, draft, []int{index})
					}
					if errors.Is(err, ui.ErrReturnToMenu) && family != "" {
						continue
					}
					break
				}
			case "5":
				updated, err = session.EditSourceRulesDraft(view, draft)
			default:
				ui.InvalidChoice()
				continue
			}
			if errors.Is(err, ui.ErrReturnToMenu) {
				continue
			}
			if err != nil {
				log.Warn(err)
				view.PauseWithPrompt("按回车返回" + session.title + "…")
				continue
			}
			if updated != draft {
				notice = "已更新草稿"
			}
			draft = updated
		}
	}
}

func (session Session) PromptPolicyPortScope(view *ui.UI, scope string, edit func([]int) error) ([]int, error) {
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
	for index, option := range []struct{ title, a, b, both, back string }{
		{"选择 IP 版本", "IPv4", "IPv6", "IPv4 + IPv6", "返回上一步"},
		{"选择协议", "TCP", "UDP", "TCP + UDP", "返回 IP 版本选择"},
	} {
		index, option := index, option
		steps = append(steps, ui.PromptStep{Run: func() error {
			ui.ClearScreen()
			ui.MenuTitle("防火墙管理", session.title, policyScopeTitle(scope), option.title)
			ui.PrintInfoCard(policyScopeTitle(scope), ui.CardField{Label: "当前步骤", Value: fmt.Sprintf("%d / 3 · %s", index+1, option.title)})
			for i, label := range []string{option.a, option.b, option.both} {
				ruleChoiceOption(strconv.Itoa(i+1), label, "", selections[index+1])
			}
			ui.MenuExit("0/q", option.back)
			choice, err := ui.AskChoice(view, "请选择", selections[index+1], map[string]string{"1": "1", "2": "2", "3": "3"})
			if err != nil {
				return err
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

func (session Session) changePolicyLists(view *ui.UI, content string, indices []int) (string, error) {
	lists := session.lists
	list := lists[indices[0]]
	title := list.Label
	if list.Family == 0 {
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
		value, err := ReadPolicyValue(content, lists[index])
		if err != nil {
			return "", err
		}
		if value == "" {
			if list.KeepSSH {
				value = "无附加业务端口，仅 SSH 端口"
			} else if session.forward {
				value = "无转发端口"
			} else {
				value = "无"
			}
		}
		fields = append(fields, ui.CardField{Label: lists[index].Label, Value: value})
	}
	if list.Family == 0 {
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
			draw := func() {
				ui.MenuTitle("防火墙管理", session.title, title, "选择操作")
				ui.PrintInfoCard("编辑所选列表", fields...)
				ui.MenuOption("1", "添加")
				if list.Family == 0 {
					if list.KeepSSH {
						ui.MenuOptionHint("2", "删除端口 / 范围", "从业务列表扣除，SSH 端口始终保留")
						ui.MenuOption("3", "替换全部业务端口")
						ui.MenuOptionHint("4", "清空业务端口", "只保留 SSH 端口")
					} else if session.forward {
						ui.MenuOptionHint("2", "删除端口 / 范围", "从转发列表扣除")
						ui.MenuOption("3", "替换全部转发端口")
						ui.MenuOptionHint("4", "清空转发端口", "清空后新的转发连接全部丢弃")
					} else {
						ui.MenuOptionHint("2", "删除端口 / 范围", "从该名单扣除，包括 SSH 端口")
						ui.MenuOption("3", "替换全部端口")
						ui.MenuOptionHint("4", "清空端口", "清空后该名单不再放行新连接")
					}
				} else {
					ui.MenuOptionHint("2", "删除地址 / 网段", "精确匹配规范化后的地址 / CIDR")
					ui.MenuOption("3", "替换整个范围")
					ui.MenuOption("4", "恢复默认内网 IP 范围")
					ui.MenuOption("5", "按序号编辑单个条目")
					ui.PrintField("范围说明", "支持单个 IP 和 CIDR，逗号分隔；自动规范化并合并重复及被包含条目；至少保留一项")
				}
				ui.MenuExit("0/q", "返回上一步")
			}
			choices := map[string]string{"1": "add", "2": "remove", "3": "replace", "4": "reset"}
			if list.Family != 0 {
				choices["5"] = "edit"
			}
			var err error
			operation, err = ui.AskMenuChoice(view, "请选择操作", "", choices, draw)
			editIndex = -1
			return err
		}},
		ui.PromptStep{Enabled: func() bool { return operation == "edit" }, Run: func() error {
			current, _ := ReadPolicyValue(content, list)
			entries := strings.Split(current, ", ")
			ui.ClearScreen()
			ui.MenuTitle("防火墙管理", session.title, title, "选择编辑条目")
			for i, entry := range entries {
				ui.MenuOption(strconv.Itoa(i+1), entry)
			}
			ui.MenuExit("0/q", "返回操作选择")
			for {
				raw, err := view.Ask("请输入要编辑的条目序号（0/q 返回）：")
				if err != nil {
					return err
				}
				if ui.IsReturnChoice(raw) {
					return ui.ErrReturnToMenu
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
			ui.MenuTitle("防火墙管理", session.title, title, action)
			inputFields := append([]ui.CardField(nil), fields...)
			if list.Family == 0 {
				inputFields[0].Value = "3 / 3 · " + action + "端口"
			}
			if editIndex >= 0 {
				current, _ := ReadPolicyValue(content, list)
				inputFields = append(inputFields, ui.CardField{Label: "编辑条目", Value: strings.Split(current, ", ")[editIndex], Detail: "回车保留当前条目"})
			}
			ui.PrintInfoCard("编辑内容", inputFields...)
			ui.MenuExit("0/q", "返回上一步")
			for {
				prompt := "请输入端口或范围，多个用逗号分隔，例如 80,443,8000-8100（0/q 返回）："
				if list.Family != 0 {
					prompt = fmt.Sprintf("请输入 IPv%d 地址 / CIDR，多个用逗号分隔（0/q 返回）：", list.Family)
				}
				if operation == "edit" {
					prompt = "请输入新的地址 / CIDR（回车保留当前条目，0/q 返回）："
				}
				raw, err := view.Ask(prompt)
				if err != nil {
					return err
				}
				if ui.IsReturnChoice(raw) {
					return ui.ErrReturnToMenu
				}
				if raw == "" {
					if operation == "edit" {
						current, _ := ReadPolicyValue(content, list)
						raw = strings.Split(current, ", ")[editIndex]
					} else {
						if list.Family == 0 {
							log.Warn("输入不能为空；清空业务端口请返回后选择 4")
						} else {
							log.Warn("输入不能为空，请填写至少一个地址或 CIDR")
						}
						continue
					}
				}
				if _, err := session.WithPolicyChanges(content, indices, operation, raw, editIndex); err != nil {
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
	return session.WithPolicyChanges(content, indices, operation, value, editIndex)
}
