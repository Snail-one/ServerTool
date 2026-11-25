package unattended

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"snail_tool/internal/input"
	"snail_tool/internal/ui"
)

const whitelistFile = "99-zzz-servertool-package-whitelist.conf"

type whitelistState struct {
	patterns []string
	strict   bool
	explicit bool
	managed  bool
}

func (service manager) whitelistPath() string {
	return filepath.Join(filepath.Dir(service.path), whitelistFile)
}

func (service manager) loadWhitelist() (whitelistState, []byte, bool, error) {
	output, err := service.query("apt-config", "dump")
	if err != nil {
		return whitelistState{}, nil, false, err
	}
	state := parseWhitelist(output)
	previous, exists, err := readManaged(service.whitelistPath(), managedHeader)
	if err != nil {
		return whitelistState{}, nil, false, err
	}
	state.managed = exists
	return state, previous, exists, nil
}

func parseWhitelist(output string) whitelistState {
	var state whitelistState
	seen := map[string]bool{}
	listPrefix := strings.ToLower(whitelistKey + "::")
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(line, " ")
		value = strings.TrimSpace(value)
		if !ok || len(value) < 3 || !strings.HasPrefix(value, `"`) || !strings.HasSuffix(value, `";`) {
			continue
		}
		literal := value[1 : len(value)-2]
		switch {
		case strings.EqualFold(key, strictListKey):
			state.explicit = true
			state.strict = switchLabel(literal) == "开启"
		case strings.HasPrefix(strings.ToLower(key), listPrefix):
			if literal == "" || seen[literal] {
				continue
			}
			seen[literal] = true
			state.patterns = append(state.patterns, literal)
		}
	}
	return state
}

func (state whitelistState) menuStatus() string {
	if len(state.patterns) == 0 {
		if state.strict {
			return "空名单 · 严格匹配"
		}
		return "按更新来源和排除规则"
	}
	if state.strict {
		return fmt.Sprintf("%d 条 · 严格匹配", len(state.patterns))
	}
	return fmt.Sprintf("%d 条", len(state.patterns))
}

func (state whitelistState) strictLabel() string {
	if state.strict {
		return "开启"
	}
	if state.explicit {
		return "关闭"
	}
	return "关闭（默认）"
}

func (state whitelistState) detail() string {
	switch {
	case len(state.patterns) == 0 && state.strict:
		return "严格匹配已开启，空名单通常会阻止自动安装更新"
	case len(state.patterns) == 0:
		return "按已允许的更新来源、软件包排除规则和 APT 策略筛选更新"
	case state.strict:
		return "只有名单中的软件包会更新，依赖也必须写在名单里"
	default:
		return "名单中的软件包及其依赖会更新"
	}
}

func (state whitelistState) clearHint() string {
	if state.strict {
		return "清空后保留严格匹配，通常会阻止自动安装更新"
	}
	return "清空后按更新来源和排除规则筛选"
}

func printWhitelistSyntax() {
	ui.PrintInfoCard("规则说明",
		ui.CardField{Label: "匹配方式", Value: "Python 正则，从软件包名开头匹配", Detail: "不必写 ^。末尾没有 $ 时，后面还可以有其他字符"},
		ui.CardField{Label: "前缀", Value: "nginx", Detail: "匹配 nginx、nginx-common"},
		ui.CardField{Label: "精确名称", Value: "nginx$", Detail: "只匹配 nginx"},
		ui.CardField{Label: "任意后缀", Value: "nginx.*", Detail: "* 只重复前一个字符，任意内容要写 .*"},
		ui.CardField{Label: "多选其一", Value: "(openssh-server|openssh-client)$"},
		ui.CardField{Label: "转义", Value: `libstdc\+\+6$`, Detail: ". + ( ) | 要按字面匹配时加反斜杠，- 不用转义"},
		ui.CardField{Label: "限制", Value: "不能包含双引号，最长 256 字节"},
	)
}

func normalizeWhitelistPattern(value string) (string, error) {
	if err := input.Text(value); err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("请输入软件包名或正则")
	}
	if len(value) > 256 {
		return "", fmt.Errorf("规则不能超过 256 个字节")
	}
	if strings.Contains(value, `"`) {
		return "", fmt.Errorf("规则不能包含双引号")
	}
	return value, nil
}

func (service manager) validateWhitelistPattern(pattern string) error {
	const script = `import re
import sys

try:
    re.compile(sys.argv[1])
except (re.error, OverflowError, RecursionError) as error:
    print(error)
`
	output, err := service.query("python3", "-I", "-c", script, pattern)
	if err != nil {
		return fmt.Errorf("无法校验 Python 正则，请检查 python3: %w", err)
	}
	if detail := strings.TrimSpace(output); detail != "" {
		return fmt.Errorf("Python 正则无效: %s", detail)
	}
	return nil
}

// renderWhitelist replaces every earlier allow-list. APT appends list values
// from all configuration files, so a later file has to clear the list before
// writing the entries that should remain effective.
func renderWhitelist(patterns []string, strict bool) string {
	var content strings.Builder
	content.WriteString(managedHeader)
	fmt.Fprintf(&content, "#clear %s;\n", whitelistKey)
	for _, pattern := range patterns {
		fmt.Fprintf(&content, "%s:: \"%s\";\n", whitelistKey, pattern)
	}
	value := "false"
	if strict {
		value = "true"
	}
	fmt.Fprintf(&content, "%s %q;\n", strictListKey, value)
	return content.String()
}

func (service manager) saveWhitelist(patterns []string, strict bool, previous []byte, exists bool) error {
	content := renderWhitelist(patterns, strict)
	return publish(service.whitelistPath(), previous, exists, content, func() error {
		output, err := service.query("apt-config", "dump")
		if err != nil {
			return fmt.Errorf("读取生效配置失败: %w", err)
		}
		effective := parseWhitelist(output)
		if !effective.explicit || effective.strict != strict || !sameStringSet(effective.patterns, patterns) {
			return fmt.Errorf("生效的软件包允许名单与预期不一致，请检查配置优先级")
		}
		return nil
	})
}

func sameStringSet(actual, expected []string) bool {
	seen := make(map[string]int, len(actual))
	for _, item := range actual {
		seen[item]++
	}
	return samePatterns(seen, expected)
}

func (service manager) whitelistMenu(view *ui.UI) error {
	for {
		ui.ClearScreen()
		state, previous, exists, err := service.loadWhitelist()
		if err != nil {
			return err
		}
		choices := map[string]string{"1": "add", "2": "delete", "3": "clear", "4": "strict"}
		if state.managed {
			choices["x"] = "release"
		}
		draw := func() {
			ui.MenuTitle("系统工具", Title, "APT 策略与开关", "软件包允许名单")
			fields := []ui.CardField{
				{Label: "名单", Value: emptyLabel(state.patterns), Detail: state.detail()},
				{Label: "严格匹配", Value: state.strictLabel()},
			}
			if state.managed {
				fields = append(fields, ui.CardField{Label: "配置来源", Value: "本工具"})
			}
			for index, pattern := range state.patterns {
				fields = append(fields, ui.CardField{Label: fmt.Sprintf("规则 %d", index+1), Value: pattern})
			}
			ui.PrintInfoCard("当前允许名单", fields...)
			fmt.Println()
			ui.MenuOptionHint("1", "添加规则", "软件包名或正则")
			ui.MenuOptionHint("2", "删除规则", "")
			ui.MenuOptionHint("3", "清空名单", state.clearHint())
			ui.MenuOptionStatus("4", "严格匹配", ui.SwitchBadge(state.strict))
			if state.managed {
				fmt.Println()
				ui.MenuShortcutHint("x", "取消接管", "恢复其他配置文件中的允许名单")
			}
			ui.MenuExit("0/q", "返回 APT 策略与开关")
		}
		choice, err := ui.AskMenuChoice(view, "请选择", "", choices, draw)
		if err != nil {
			return err
		}
		ui.RunAction(view, "修改允许名单失败", func() error {
			switch choice {
			case "add":
				return service.addWhitelistPattern(view, state, previous, exists)
			case "delete":
				return service.deleteWhitelistPattern(view, state, previous, exists)
			case "clear":
				return service.clearWhitelist(view, state, previous, exists)
			case "strict":
				return service.editWhitelistStrict(view, state, previous, exists)
			case "release":
				return service.releaseWhitelist(view, previous, exists)
			}
			return nil
		})
	}
}

func containsPattern(patterns []string, pattern string) bool {
	for _, current := range patterns {
		if current == pattern {
			return true
		}
	}
	return false
}

func emptyLabel(patterns []string) string {
	if len(patterns) == 0 {
		return "空名单"
	}
	return fmt.Sprintf("%d 条", len(patterns))
}

func (service manager) addWhitelistPattern(view *ui.UI, state whitelistState, previous []byte, exists bool) error {
	if err := service.requireWhitelistRoot(); err != nil {
		return err
	}
	showPage("添加允许规则")
	printWhitelistSyntax()
	fmt.Println()
	value, err := view.Ask("请输入软件包名或正则（0/q 返回）：")
	if err != nil {
		return err
	}
	if ui.IsReturnChoice(value) {
		return ui.ErrReturnToMenu
	}
	pattern, err := normalizeWhitelistPattern(value)
	if err != nil {
		return err
	}
	if containsPattern(state.patterns, pattern) {
		return fmt.Errorf("这条规则已经在允许名单中")
	}
	if err := service.validateWhitelistPattern(pattern); err != nil {
		return err
	}
	next := append(append([]string{}, state.patterns...), pattern)
	return service.confirmWhitelist(view, state, previous, exists, next, state.strict, "添加规则 "+pattern)
}

func (service manager) deleteWhitelistPattern(view *ui.UI, state whitelistState, previous []byte, exists bool) error {
	if err := service.requireWhitelistRoot(); err != nil {
		return err
	}
	if len(state.patterns) == 0 {
		return fmt.Errorf("允许名单是空的")
	}
	showPage("删除允许规则")
	fields := make([]ui.CardField, 0, len(state.patterns))
	for index, pattern := range state.patterns {
		fields = append(fields, ui.CardField{Label: strconv.Itoa(index + 1), Value: pattern})
	}
	ui.PrintInfoCard("当前规则", fields...)
	fmt.Println()
	number, err := askWhitelistIndex(view, len(state.patterns))
	if err != nil {
		return err
	}
	pattern := state.patterns[number-1]
	next := append([]string{}, state.patterns[:number-1]...)
	next = append(next, state.patterns[number:]...)
	return service.confirmWhitelist(view, state, previous, exists, next, state.strict, "删除规则 "+pattern)
}

func askWhitelistIndex(view *ui.UI, count int) (int, error) {
	value, err := view.Ask("请输入要删除的编号（0/q 返回）：")
	if err != nil {
		return 0, err
	}
	if ui.IsReturnChoice(value) {
		return 0, ui.ErrReturnToMenu
	}
	number, err := input.Number(value, 1, count)
	if err != nil {
		return 0, err
	}
	return number, nil
}

func (service manager) clearWhitelist(view *ui.UI, state whitelistState, previous []byte, exists bool) error {
	if err := service.requireWhitelistRoot(); err != nil {
		return err
	}
	if len(state.patterns) == 0 {
		return fmt.Errorf("允许名单是空的")
	}
	return service.confirmWhitelist(view, state, previous, exists, nil, state.strict, "清空允许名单")
}

func (service manager) editWhitelistStrict(view *ui.UI, state whitelistState, previous []byte, exists bool) error {
	if err := service.requireWhitelistRoot(); err != nil {
		return err
	}
	current := "关闭"
	if state.strict {
		current = "开启"
	}
	value, err := askToggle(view, current, "true", "false", "", func() {
		showPage("严格匹配")
		ui.PrintInfoCard("当前设置", ui.CardField{Label: "严格匹配", Value: state.strictLabel(), Detail: state.detail()})
		fmt.Println()
	})
	if err != nil {
		return err
	}
	strict := value == "true"
	if strict == state.strict {
		return ui.ErrReturnToMenu
	}
	action := "关闭严格匹配"
	if strict {
		action = "开启严格匹配"
	}
	return service.confirmWhitelist(view, state, previous, exists, state.patterns, strict, action)
}

func (service manager) requireWhitelistRoot() error {
	if !service.root() {
		return fmt.Errorf("修改软件包允许名单需要 root 权限")
	}
	return nil
}

func (service manager) confirmWhitelist(view *ui.UI, current whitelistState, previous []byte, exists bool, patterns []string, strict bool, action string) error {
	if err := service.requireWhitelistRoot(); err != nil {
		return err
	}
	next := whitelistState{patterns: patterns, strict: strict, explicit: true, managed: true}
	showPage("确认允许名单")
	fields := []ui.CardField{
		{Label: "操作", Value: action, Changed: true},
		{Label: "名单", Value: emptyLabel(patterns), Detail: next.detail()},
		{Label: "严格匹配", Value: next.strictLabel(), Changed: strict != current.strict},
		{Label: "生效方式", Value: "写入本工具配置，并覆盖其他文件中的允许名单"},
	}
	for index, pattern := range patterns {
		fields = append(fields, ui.CardField{Label: fmt.Sprintf("规则 %d", index+1), Value: pattern, Changed: !containsPattern(current.patterns, pattern)})
	}
	ui.PrintInfoCard("将应用的允许名单", fields...)
	fmt.Println()
	if err := confirm(view, "确认保存允许名单？(y/N)："); err != nil {
		return err
	}
	if err := service.saveWhitelist(patterns, strict, previous, exists); err != nil {
		return err
	}
	ui.PrintSuccessCard("允许名单已保存", fields[1:]...)
	return nil
}

func (service manager) releaseWhitelist(view *ui.UI, previous []byte, exists bool) error {
	if err := service.requireWhitelistRoot(); err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("当前没有由本工具接管的允许名单")
	}
	showPage("取消接管允许名单")
	ui.PrintInfoCard("将移除的配置",
		ui.CardField{Label: "配置文件", Value: service.whitelistPath()},
		ui.CardField{Label: "恢复结果", Value: "其他 APT 配置中的允许名单重新生效"},
	)
	fmt.Println()
	if err := confirm(view, "确认取消接管允许名单？(y/N)："); err != nil {
		return err
	}
	if err := publish(service.whitelistPath(), previous, exists, "", service.validateAPT); err != nil {
		return err
	}
	ui.PrintSuccessCard("已取消接管允许名单", ui.CardField{Label: "配置文件", Value: service.whitelistPath()})
	return nil
}
