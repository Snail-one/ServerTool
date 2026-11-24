package portmapping

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/input"
	"snail_tool/internal/log"
	"snail_tool/internal/ui"
)

type Service struct {
	Store            configuration.Store
	HasManagedConfig func(string) bool
	ForwardingStatus func() ui.CardField
}

func (service Service) Run(view *ui.UI) error {
	invalidChoice := false
	for {
		version, err := service.Store.Read()
		if err != nil {
			return err
		}
		content := string(version.Content)
		if !service.HasManagedConfig(content) {
			return fmt.Errorf("请先选择 d 生成本工具防火墙配置")
		}
		mappings, err := Read(content)
		if err != nil {
			return err
		}
		Page("")
		printStatus(mappings, service.ForwardingStatus())
		printEntries(mappings)
		ui.MenuShortcutHint("n", "新建端口映射", "")
		ui.MenuExit("0/q", "返回防火墙管理")
		prompt := "请选择："
		if len(mappings) > 0 {
			prompt = "请输入映射序号或操作："
		}
		if invalidChoice {
			ui.InvalidChoice()
			invalidChoice = false
		}
		choice, err := view.Ask(prompt)
		if err != nil {
			return err
		}
		if ui.IsReturnChoice(choice) {
			return ui.ErrReturnToMenu
		}
		number, numberErr := input.Number(choice, 1, len(mappings))
		switch {
		case strings.EqualFold(choice, "n"):
			service.runChange(view, version, mappings, len(mappings), "新建")
		case numberErr == nil:
			ui.RunAction(view, "打开端口映射失败，已返回映射列表", func() error {
				return service.runDetails(view, version, mappings, number-1)
			})
		default:
			invalidChoice = true
		}
	}
}

func Page(stage string) {
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "端口映射管理", stage)
}

func printStatus(mappings []Mapping, forwarding ui.CardField) {
	enabled, snatEnabled := 0, 0
	for _, mapping := range mappings {
		if !mapping.disabled {
			enabled++
		}
		if mapping.snatEnabled() {
			snatEnabled++
		}
	}
	ui.PrintInfoCard("端口映射状态",
		forwarding,
		ui.CardField{Label: "已保存映射", Value: fmt.Sprintf("共 %d 条 · 已启用 %d 条 · 已停用 %d 条", len(mappings), enabled, len(mappings)-enabled)},
		ui.CardField{Label: "SNAT 配置", Value: fmt.Sprintf("已启用 %d 条", snatEnabled)},
	)
}

func printEntries(entries []Mapping) {
	fmt.Println()
	if len(entries) == 0 {
		fmt.Println("尚未配置端口映射")
		fmt.Println()
		return
	}
	fmt.Println(ui.InfoText("   " + ui.TableCell("序号", 6) + ui.TableCell("IP 版本", 9) +
		ui.TableCell("协议", 10) + ui.TableCell("状态", 9) + "本机端点 → 目标端点"))
	for i, mapping := range entries {
		endpoints := mapping.endpointsLabel()
		if mapping.snat.address.IsValid() {
			endpoints += " · SNAT " + ui.StateText(mapping.snatDescription(), mapping.snatEnabled())
		}
		fmt.Println("   " + ui.TableCell(strconv.Itoa(i+1), 6) + ui.TableCell(mapping.familyLabel(), 9) +
			ui.TableCell(configuration.ProtocolLabel(mapping.protocol), 10) + ui.TableCell(ui.StateText(mapping.statusLabel(), !mapping.disabled), 9) + endpoints)
		if matches := mapping.matchLabels(); len(matches) > 0 {
			fmt.Println("         " + strings.Join(matches, " · "))
		}
	}
	fmt.Println()
}

func (mapping Mapping) statusLabel() string {
	if mapping.disabled {
		return "已停用"
	}
	return "已启用"
}

func (service Service) runDetails(view *ui.UI, version configuration.Version, mappings []Mapping, index int) error {
	for {
		mapping := mappings[index]
		Page(fmt.Sprintf("映射 %d", index+1))
		iface, source := mapping.iface, "不限"
		if iface == "" {
			iface = "不限"
		}
		if mapping.source.IsValid() {
			source = mapping.source.String()
		}
		snat := ui.CardField{Label: "SNAT", Value: mapping.snatDescription()}
		if mapping.snat.address.IsValid() && !mapping.snatEnabled() {
			snat.Detail = fmt.Sprintf("出站网卡 %s · 源 IP %s", mapping.snat.iface, mapping.snat.address)
		}
		ui.PrintInfoCard("端口映射配置",
			ui.CardField{Label: "状态", Value: mapping.statusLabel()},
			ui.CardField{Label: "协议", Value: mapping.familyLabel() + " " + configuration.ProtocolLabel(mapping.protocol)},
			ui.CardField{Label: "本机端点", Value: mapping.from.String()},
			ui.CardField{Label: "目标端点", Value: mapping.to.String()},
			ui.CardField{Label: "入站网卡", Value: iface},
			ui.CardField{Label: "来源", Value: source},
			snat,
		)
		fmt.Println()
		toggle := "停用"
		if mapping.disabled {
			toggle = "启用"
		}
		ui.MenuOption("1", "编辑映射")
		ui.MenuOptionStatus("2", toggle+"映射", ui.SoftwareBadge(mapping.statusLabel(), !mapping.disabled))
		ui.MenuOptionStatus("3", "SNAT 设置", mapping.snatBadge())
		ui.MenuOption("4", "删除映射")
		ui.MenuExit("0/q", "返回映射列表")
		action, err := ui.AskChoice(view, "请选择", "", map[string]string{"1": "编辑", "2": toggle, "3": "SNAT", "4": "删除"})
		if err != nil {
			return err
		}
		updated := service.runChange(view, version, mappings, index, action)
		if updated == "" {
			continue
		}
		if action == "删除" {
			return ui.ErrReturnToMenu
		}
		version.Content = []byte(updated)
		mappings, err = Read(updated)
		if err != nil {
			return err
		}
	}
}

func (service Service) runChange(view *ui.UI, version configuration.Version, mappings []Mapping, index int, action string) string {
	updated, err := service.change(view, version, mappings, index, action)
	if errors.Is(err, ui.ErrReturnToMenu) {
		return updated
	}
	if err != nil {
		log.Error(err)
	}
	destination := "映射详情"
	if action == "新建" || action == "删除" && updated != "" {
		destination = "映射列表"
	}
	view.PauseWithPrompt("按回车返回" + destination + "…")
	return updated
}

// Return the written configuration even if loading fails so navigation and
// subsequent changes use the saved rules.
func (service Service) change(view *ui.UI, version configuration.Version, mappings []Mapping, index int, action string) (string, error) {
	var current Mapping
	if index < len(mappings) {
		current = mappings[index]
	}

	mapping := current
	switch action {
	case "新建", "编辑":
		Page(action + "端口映射")
		if action == "编辑" {
			ui.PrintInfoCard("当前选择", ui.CardField{Label: "端口映射", Value: current.description()})
		}
		updated, err := askPortMapping(view, current)
		if err != nil {
			return "", err
		}
		mapping = updated
		if action == "编辑" && mapping == current {
			return "", ui.ErrReturnToMenu
		}
	case "SNAT":
		Page("SNAT 设置")
		updated, change, err := askPortMappingSNAT(view, current)
		if err != nil {
			return "", err
		}
		if updated == current.snat {
			return "", ui.ErrReturnToMenu
		}
		mapping.snat, action = updated, change
	case "启用", "停用":
		mapping.disabled = action == "停用"
	}
	mappings = slices.Clone(mappings)
	if action == "删除" {
		mappings = slices.Delete(mappings, index, index+1)
	} else {
		for i, existing := range mappings {
			if i != index && mapping.overlaps(existing) {
				return "", fmt.Errorf("与第 %d 条映射的匹配条件重叠：%s", i+1, existing.description())
			}
		}
		if index == len(mappings) {
			mappings = append(mappings, mapping)
		} else {
			mappings[index] = mapping
		}
	}

	updated, err := Replace(string(version.Content), mappings)
	if err != nil {
		return "", err
	}

	Page("确认端口映射变更")
	ui.PrintInfoCard("端口映射变更",
		ui.CardField{Label: "操作", Value: action + " " + mapping.description()},
		ui.CardField{Label: "转发条件", Value: "远程目标需要开启内核转发、放行转发后的目标端口，回包须经本机"},
		ui.CardField{Label: "生效方式", Value: "保存并加载整份防火墙配置，映射用于新连接"},
	)
	confirmed, err := view.Confirm("确认保存并加载？(y/N，0/q 返回)：")
	if err != nil {
		return "", err
	}
	if !confirmed {
		return "", ui.ErrReturnToMenu
	}
	saved, err := service.Store.SaveAndApply(version, updated)
	if err != nil {
		if !saved {
			return "", err
		}
		return updated, fmt.Errorf("端口映射已保存，但未成功加载，可返回防火墙管理输入 r 重试: %w", err)
	}
	ui.PrintSuccessCard("端口映射已保存并加载")
	return updated, nil
}

func askPortMapping(view *ui.UI, current Mapping) (Mapping, error) {
	ui.PrintSectionCard("输入格式",
		ui.CardField{Label: "顺序", Value: "协议 本机IP:端口 目标IP:端口 [入站网卡] [来源IP/CIDR]", Detail: "用空格分开，方括号内的两个字段可省略"},
		ui.CardField{Label: "协议", Value: "tcp、udp、both（TCP 和 UDP）"},
		ui.CardField{Label: "地址", Value: "入口用 * 匹配同一地址族的所有本机地址。目标填实际接收地址", Detail: "两端地址族须一致，IPv6 用 [地址]:端口，例如 [fd00::20]:80"},
		ui.CardField{Label: "端口", Value: "支持单端口或连续范围。目标可填单端口或等长范围"},
		ui.CardField{Label: "匹配条件", Value: "网卡和来源同时填写时，两者都必须匹配", Detail: "只限制来源时，用 - 占住入站网卡的位置"},
		ui.CardField{Label: "出站网卡", Value: "由系统路由选择，可在「2 转发规则管理 → 5 规则配置」限制放行"},
	)
	ui.PrintSectionCard("配置示例",
		ui.CardField{Label: "不限网卡和来源", Value: "tcp *:80 192.168.1.20:80"},
		ui.CardField{Label: "只限入站网卡", Value: "tcp *:80 192.168.1.20:80 eth0"},
		ui.CardField{Label: "只限来源网段", Value: "tcp *:80 192.168.1.20:80 - 192.168.1.0/24"},
		ui.CardField{Label: "同时限制两项", Value: "tcp *:80 192.168.1.20:80 eth0 192.168.1.0/24"},
	)
	line, err := view.Ask("请输入端口映射（0/q 返回）：")
	if err != nil {
		return Mapping{}, err
	}
	if ui.IsReturnChoice(line) {
		return Mapping{}, ui.ErrReturnToMenu
	}
	fields := strings.Fields(line)
	if len(fields) < 3 || len(fields) > 5 {
		return Mapping{}, fmt.Errorf("请输入协议、本机端点、目标端点和可选的入站网卡、来源地址")
	}
	config := portMappingConfig{Protocol: fields[0], From: fields[1], To: fields[2], Disabled: current.disabled, SNAT: current.snat.config()}
	if len(fields) >= 4 && fields[3] != "-" {
		config.Iface = fields[3]
	}
	if len(fields) == 5 && fields[4] != "-" {
		config.Source = fields[4]
	}
	return config.parse()
}
