package firewall

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

func printRuleOrder(rules []allowRule, selected string) {
	fmt.Println()
	if len(rules) == 0 {
		fmt.Println("没有附加放行规则")
		fmt.Println()
		return
	}
	fmt.Println(ui.InfoText("   " + ui.TableCell("顺序", 6) + ui.TableCell("规则 ID", 11) + ui.TableCell("协议", 10) + ui.TableCell("来源", 43) + "端口"))
	for index, rule := range rules {
		source, ports := rule.Source, rule.Ports
		if source == "" {
			source = "任意来源"
		}
		if ports == "" {
			ports = "全部端口"
		}
		prefix := "   "
		if rule.ID == selected {
			prefix = ui.PrimaryText(" > ")
		}
		fmt.Println(prefix + ui.TableCell(strconv.Itoa(index+1), 6) + ui.TableCell(rule.ID, 11) + ui.TableCell(protocolLabel(rule.Protocol), 10) + ui.TableCell(source, 43) + ports)
	}
	fmt.Println()
}

func reorderAllowRules(view *ui.UI, version configVersion, rules []allowRule) error {
	if len(rules) < 2 {
		ui.PrintInfoCard("无需调整顺序", ui.CardField{Label: "规则数量", Value: "至少需要两条附加规则"})
		return nil
	}
	draft := append([]allowRule(nil), rules...)
	selected := draft[0].ID
	notice := ""
	for {
		ui.ClearScreen()
		ui.MenuTitle("防火墙管理", "放行规则管理", "调整规则顺序")
		state := "与已保存顺序一致"
		if !reflect.DeepEqual(draft, rules) {
			state = "有未保存的调整"
		}
		ui.PrintInfoCard("规则顺序草稿",
			ui.CardField{Label: "状态", Value: state},
			ui.CardField{Label: "选中规则", Value: selected},
			ui.CardField{Label: "匹配顺序", Value: "从上到下；TCP+UDP 规则作为一个整体移动"},
			ui.CardField{Label: "调整范围", Value: "仅调整本页附加规则，移动后保存才生效"},
		)
		printRuleOrder(draft, selected)
		ui.PrintField("选择规则", "输入列表序号或完整规则 ID")
		ui.MenuOption("u", "上移")
		ui.MenuOption("d", "下移")
		ui.MenuOption("t", "置顶")
		ui.MenuOption("b", "置底")
		ui.MenuOption("r", "还原草稿")
		ui.MenuShortcutHint("s", "保存并立即加载", "统一校验、备份并保存当前顺序")
		ui.MenuExit("0/q", "放弃草稿并返回")
		if notice != "" {
			ui.PrintField("提示", notice)
			notice = ""
		}
		choice, err := view.Ask("请选择操作或规则：")
		if err != nil {
			return err
		}
		if shared.IsReturnChoice(choice) {
			return shared.ErrReturnToMenu
		}
		choice = strings.ToLower(choice)
		from := 0
		found := false
		for index, rule := range draft {
			if choice == rule.ID {
				selected, found = rule.ID, true
			}
			if rule.ID == selected {
				from = index
			}
		}
		if found {
			continue
		}
		if number, err := strconv.Atoi(choice); err == nil && number >= 1 && number <= len(draft) {
			selected = draft[number-1].ID
			continue
		}
		switch choice {
		case "r":
			draft = append([]allowRule(nil), rules...)
			notice = "已还原为进入页面时的顺序"
			continue
		case "s":
			if reflect.DeepEqual(draft, rules) {
				notice = "顺序未变化，无需保存"
				continue
			}
			confirmed, err := view.Confirm("确认保存当前规则顺序并立即加载？(y/N)：")
			if errors.Is(err, shared.ErrReturnToMenu) || err == nil && !confirmed {
				notice = "尚未保存，可继续调整或返回放弃草稿"
				continue
			}
			if err != nil {
				return err
			}
			if err := saveManagedRules(version, draft); err != nil {
				return err
			}
			ui.PrintSuccessCard("规则顺序已保存并加载", ui.CardField{Label: "规则数量", Value: fmt.Sprintf("%d 条", len(draft))})
			return nil
		case "u", "d", "t", "b":
			to := from
			switch choice {
			case "u":
				to--
			case "d":
				to++
			case "t":
				to = 0
			case "b":
				to = len(draft) - 1
			}
			if to < 0 || to >= len(draft) || to == from {
				notice = "已在该方向的边界，顺序保持原样"
				continue
			}
			moving := draft[from]
			if to < from {
				copy(draft[to+1:from+1], draft[to:from])
			} else {
				copy(draft[from:to], draft[from+1:to+1])
			}
			draft[to] = moving
		default:
			notice = "请输入有效序号、规则 ID 或操作快捷键"
		}
	}
}
