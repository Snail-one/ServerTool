package firewall

import (
	_ "embed"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

//go:embed nftables.conf
var configTemplate string

func configure(view *ui.UI) error {
	if !isRoot() {
		return fmt.Errorf("防火墙配置需要 root 权限，请使用 sudo 运行本工具")
	}
	installed, err := ensureNftables(view)
	if err != nil {
		return err
	}
	if !installed {
		return shared.ErrReturnToMenu
	}
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "生成配置")
	version, err := readConfigVersion()
	if err != nil {
		return err
	}
	firstConfiguration := !hasManagedConfig(string(version.content))
	var firstAutostart *autostartStatus
	autostartAction := "保持当前设置"
	if firstConfiguration {
		firstAutostart, err = prepareFirstAutostart()
		if err != nil {
			return err
		}
		autostartAction = "首次配置加载成功后自动开启"
		if firstAutostart == nil {
			autostartAction = "不支持自动设置（未找到 systemctl）"
		}
	}
	mappings, err := readPortMappings(string(version.content))
	if err != nil {
		return err
	}
	var preserved, preservedForward []allowRule
	countersEnabled, err := packetCountersEnabled(string(version.content))
	if err != nil {
		return err
	}
	pingEnabled, pingFound, err := readPingSetting(string(version.content))
	if err != nil {
		return err
	}
	dockerBridge, dockerBridgeFound, err := readDockerBridge(string(version.content))
	if err != nil {
		return err
	}
	var droppedAccess, droppedForward int
	if strings.Contains(string(version.content), rulesBegin) || strings.Contains(string(version.content), rulesEnd) {
		preserved, err = managedRules(string(version.content))
		if err != nil {
			return err
		}
		preserved, droppedAccess = omitUnconvertedPortRules(preserved)
	}
	if strings.Contains(string(version.content), forwardRulesBegin) || strings.Contains(string(version.content), forwardRulesEnd) {
		preservedForward, err = managedRulesIn(string(version.content), forwardRuleBlock)
		if err != nil {
			return err
		}
		preservedForward, droppedForward = omitUnconvertedPortRules(preservedForward)
	}
	ports, err := detectSSHPorts()
	if err != nil {
		return err
	}
	if len(ports) > 1 {
		log.Warn("SSH 生效配置包含多个端口：", portList(ports), "。通常建议只保留一个 SSH 端口。选择开放的范围会同时写入这些端口，请在 SSH 安全配置中检查并关闭不再使用的旧端口。")
	}
	sshFields := []ui.CardField{
		ui.CardField{Label: "端口", Value: portList(ports), Detail: "/usr/sbin/sshd -T"},
		ui.CardField{Label: "放行协议", Value: "相同端口号同时放行 TCP / UDP，IPv4 / IPv6 同时应用"},
		ui.CardField{Label: "附加放行规则", Value: fmt.Sprintf("保留已有 %d 条规则", len(preserved))},
	}
	if text := unconvertedPortRuleText(droppedAccess, droppedForward); text != "" {
		sshFields = append(sshFields, ui.CardField{Label: "旧任意来源规则", Value: text})
	}
	if len(ports) > 1 {
		sshFields = append(sshFields, ui.CardField{Label: "SSH 提示", Value: "检测到多个 SSH 端口。选择开放的范围会同时写入这些端口；建议在 SSH 安全配置中检查并关闭不再使用的旧端口"})
	}
	var content string
	var publicSSH, privateSSH bool
	for {
		publicWeb, privateWeb, write := false, false, false
		publicSSH, privateSSH = false, false
		err = ui.RunPromptSteps(
			ui.PromptStep{Run: func() error {
				var err error
				publicSSH, err = askSSH(view, "公网", portList(ports), append([]ui.CardField(nil), sshFields...)...)
				return err
			}},
			ui.PromptStep{Run: func() error {
				fields := append([]ui.CardField(nil), sshFields...)
				fields = append(fields, ui.CardField{Label: "已选公网 SSH", Value: sshChoiceLabel(publicSSH)})
				var err error
				privateSSH, err = askSSH(view, "局域网", portList(ports), fields...)
				return err
			}},
			ui.PromptStep{Run: func() error {
				fields := append([]ui.CardField(nil), sshFields...)
				fields = append(fields,
					ui.CardField{Label: "已选公网 SSH", Value: sshChoiceLabel(publicSSH)},
					ui.CardField{Label: "已选局域网 SSH", Value: sshChoiceLabel(privateSSH)},
				)
				var err error
				publicWeb, err = askWeb(view, "公网", fields...)
				return err
			}},
			ui.PromptStep{Run: func() error {
				fields := append([]ui.CardField(nil), sshFields...)
				publicState := "关闭"
				if publicWeb {
					publicState = "开放 80、443"
				}
				fields = append(fields,
					ui.CardField{Label: "已选公网 SSH", Value: sshChoiceLabel(publicSSH)},
					ui.CardField{Label: "已选局域网 SSH", Value: sshChoiceLabel(privateSSH)},
					ui.CardField{Label: "已选公网 Web", Value: publicState},
				)
				var err error
				privateWeb, err = askWeb(view, "局域网", fields...)
				return err
			}},
			ui.PromptStep{Run: func() error {
				content, err = buildScopedConfig(ports, publicSSH, privateSSH, publicWeb, privateWeb)
				if err != nil {
					return err
				}
				content, err = replaceManagedRules(content, preserved)
				if err != nil {
					return err
				}
				content, err = replaceManagedRulesIn(content, preservedForward, forwardRuleBlock)
				if err != nil {
					return err
				}
				if version.exists && !pingFound {
					content, err = withoutPingSetting(content)
				} else {
					content, err = withPingSetting(content, pingEnabled)
				}
				if err != nil {
					return err
				}
				content, err = withPortMappings(content, mappings)
				if err != nil {
					return err
				}
				content, err = withPacketCounters(content, countersEnabled)
				if err != nil {
					return err
				}
				if dockerBridgeFound {
					content, _, err = withDockerBridge(content, dockerBridge)
					if err != nil {
						return err
					}
				}
				content, err = applyDockerForwardMarkIfEnabled(content)
				if err != nil {
					return err
				}
				var err error
				write, err = confirmConfig(view, &content, autostartAction, droppedAccess, droppedForward)
				return err
			}},
		)
		if err != nil {
			return err
		}
		if write {
			break
		}
	}
	// Detect again after the interaction so a changed SSH listener cannot be
	// silently omitted from the generated firewall.
	latest, err := detectSSHPorts()
	if err != nil {
		return err
	}
	if portList(latest) != portList(ports) {
		return fmt.Errorf("SSH 配置端口已变化，请重新进入防火墙管理生成配置")
	}
	if firstAutostart != nil {
		if err := checkAutostartVersion(*firstAutostart); err != nil {
			return err
		}
	}
	saved, err := saveAndApplyConfig(version, content)
	if err != nil {
		if !saved {
			return err
		}
		return fmt.Errorf("配置已写入 %s，但未成功加载: %w", configPath, err)
	}
	if firstAutostart != nil {
		if err := enableFirstAutostart(*firstAutostart, []byte(content)); err != nil {
			return fmt.Errorf("防火墙配置已写入并加载，但自动开启开机自启未完成，请在常用功能设置中重试: %w", err)
		}
		autostartAction = "已开启"
	} else if firstConfiguration {
		log.Warn("当前系统未找到 systemctl，防火墙配置已加载，未自动设置开机自启")
	}
	ui.PrintSuccessCard("nftables 配置已写入并加载",
		ui.CardField{Label: "配置文件", Value: configPath},
		ui.CardField{Label: "SSH 端口", Value: portList(ports)},
		ui.CardField{Label: "公网 SSH", Value: sshChoiceLabel(publicSSH)},
		ui.CardField{Label: "局域网 SSH", Value: sshChoiceLabel(privateSSH)},
		ui.CardField{Label: "当前状态", Value: "配置已校验并立即生效"},
		ui.CardField{Label: "开机自启", Value: autostartAction},
		ui.CardField{Label: "查看规则", Value: "选择 4 查看端口策略，选择 5 查看全部规则"},
	)
	return nil
}

func askSSH(view *ui.UI, scope, ports string, fields ...ui.CardField) (bool, error) {
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "生成配置", scope+" SSH 端口")
	fields = append(append([]ui.CardField(nil), fields...), ui.CardField{Label: "本步设置", Value: scope + " TCP / UDP " + ports + "，IPv4 / IPv6 同时应用"})
	ui.PrintInfoCard("生成基础配置", fields...)
	ui.MenuOptionHint("y", "开放 SSH 端口", "默认，直接回车选择")
	ui.MenuOption("n", "保持关闭")
	backLabel := "返回防火墙管理"
	if scope == "局域网" {
		backLabel = "返回公网 SSH 端口选择"
	}
	ui.MenuExit("0/q", backLabel)
	for {
		choice, err := view.Ask(fmt.Sprintf("是否开放%s SSH 端口 %s（TCP / UDP、IPv4 / IPv6 同时开放）？[Y/n，0/q 返回]：", scope, ports))
		if err != nil {
			return false, err
		}
		if shared.IsReturnChoice(choice) {
			return false, shared.ErrReturnToMenu
		}
		switch strings.ToLower(choice) {
		case "", "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Println("请输入 y 或 n，或输入 0/q 返回")
		}
	}
}

func sshChoiceLabel(open bool) string {
	if open {
		return "开放"
	}
	return "关闭"
}

func askWeb(view *ui.UI, scope string, fields ...ui.CardField) (bool, error) {
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "生成配置", scope+" Web 端口")
	fields = append(fields, ui.CardField{Label: "本步设置", Value: scope + " TCP / UDP 80、443，IPv4 / IPv6 同时应用"})
	ui.PrintInfoCard("生成基础配置", fields...)
	ui.MenuOption("y", "开放 Web 端口 80、443")
	ui.MenuOptionHint("n", "保持关闭", "默认，直接回车选择")
	backLabel := "返回局域网 SSH 端口选择"
	if scope == "局域网" {
		backLabel = "返回公网 Web 端口选择"
	}
	ui.MenuExit("0/q", backLabel)
	for {
		choice, err := view.Ask(fmt.Sprintf("是否开放%s Web 80、443（TCP / UDP、IPv4 / IPv6 同时开放）？[y/N，0/q 返回]：", scope))
		if err != nil {
			return false, err
		}
		if shared.IsReturnChoice(choice) {
			return false, shared.ErrReturnToMenu
		}
		switch strings.ToLower(choice) {
		case "y", "yes":
			return true, nil
		case "", "n", "no":
			return false, nil
		default:
			fmt.Println("请输入 y 或 n，或输入 0/q 返回")
		}
	}
}

// Kind port is the old any-source port exception. Rebuild keeps source rules and
// full rules. An exception the user has not moved into the base port lists is omitted.
func omitUnconvertedPortRules(rules []allowRule) ([]allowRule, int) {
	kept := make([]allowRule, 0, len(rules))
	dropped := 0
	for _, rule := range rules {
		if rule.Kind == "port" {
			dropped++
			continue
		}
		kept = append(kept, rule)
	}
	return kept, dropped
}

func unconvertedPortRuleText(access, forward int) string {
	switch {
	case access > 0 && forward > 0:
		return fmt.Sprintf("未转换的访问 %d 条、转发 %d 条不保留，生成配置时覆盖", access, forward)
	case access > 0:
		return fmt.Sprintf("未转换的 %d 条不保留，生成配置时覆盖", access)
	case forward > 0:
		return fmt.Sprintf("未转换的转发 %d 条不保留，生成配置时覆盖", forward)
	default:
		return ""
	}
}

// A false result lets the caller return to the SSH and web-port choices; cancellation
// uses ErrReturnToMenu so the generation wizard returns to its preceding step.
func confirmConfig(view *ui.UI, draft *string, autostartAction string, droppedAccess, droppedForward int) (bool, error) {
	for {
		content := *draft
		ui.ClearScreen()
		ui.MenuTitle("防火墙管理", "确认配置")
		extraRules, err := managedRules(content)
		if err != nil {
			return false, err
		}
		pingEnabled, pingFound, err := readPingSetting(content)
		if err != nil {
			return false, err
		}
		pingState := "沿用原有规则"
		if pingFound {
			pingState = pingLabel(pingEnabled)
		}
		counters, err := packetCountersEnabled(content)
		if err != nil {
			return false, err
		}
		counterState := "已关闭"
		if counters {
			counterState = ui.PrimaryText("已开启")
		}
		ui.PrintInfoCard("访问规则", accessSummaryFields(content)...)
		ui.PrintInfoCard("转发规则", forwardSummaryFields(content)...)
		sshPorts, err := sshPortText(content)
		if err != nil {
			return false, err
		}
		writeFields := []ui.CardField{
			{Label: "SSH 端口", Value: sshPorts},
			{Label: "附加放行规则", Value: fmt.Sprintf("保留 %d 条，可额外放行来源和端口", len(extraRules))},
		}
		if text := unconvertedPortRuleText(droppedAccess, droppedForward); text != "" {
			writeFields = append(writeFields, ui.CardField{Label: "旧任意来源规则", Value: text})
		}
		writeFields = append(writeFields,
			ui.CardField{Label: "ICMP / ICMPv6 回显", Value: pingState},
			ui.CardField{Label: "数据包统计", Value: counterState},
			ui.CardField{Label: "替换范围", Value: "使用当前草稿替换原配置；原端口、内网 IP 范围及手写规则不会自动沿用"},
			ui.CardField{Label: "内核规则", Value: "重建 inet snail 表，保留其他表"},
			ui.CardField{Label: "未匹配入站流量", Value: "丢弃；保留已建立连接、回环、指定 IPv6 控制流量及 DHCPv6"},
			ui.CardField{Label: "未匹配转发流量", Value: "丢弃；已建立连接放行，新连接按转发规则名单匹配目标端口"},
			ui.CardField{Label: "配置文件", Value: configPath, Detail: "整份替换，已有文件先备份；校验成功后写入"},
			ui.CardField{Label: "生效方式", Value: "写入后自动加载，立即生效"},
			ui.CardField{Label: "开机自启", Value: autostartAction},
		)
		ui.PrintInfoCard("即将写入的配置", writeFields...)
		fmt.Println()
		ui.MenuOption("1", "校验、写入并立即加载")
		ui.MenuOption("2", "预览完整配置")
		ui.MenuOption("3", "重新选择 SSH / Web 端口")
		ui.MenuOptionHint("4", "自定义访问规则", "端口 / 内网 IP 范围 / 规则配置，先调整草稿")
		ui.MenuOptionHint("5", "自定义转发规则", "转发端口 / 转发内网范围 / 规则配置，先调整草稿")
		ui.MenuExit("0/q", "返回局域网 Web 端口选择")
		choice, err := view.Ask("请选择：")
		if err != nil {
			return false, err
		}
		if shared.IsReturnChoice(choice) {
			return false, shared.ErrReturnToMenu
		}
		switch choice {
		case "1":
			return true, nil
		case "2":
			ui.ClearScreen()
			ui.MenuTitle("防火墙管理", "配置预览")
			fmt.Println(content)
			view.PauseWithPrompt("按回车返回配置确认…")
		case "3":
			return false, nil
		case "4":
			updated, err := accessPolicy.editPolicyDraft(view, content, true, nil)
			if errors.Is(err, shared.ErrReturnToMenu) {
				continue
			}
			if err != nil {
				return false, err
			}
			*draft = updated
		case "5":
			updated, err := forwardPolicy.editPolicyDraft(view, content, true, nil)
			if errors.Is(err, shared.ErrReturnToMenu) {
				continue
			}
			if err != nil {
				return false, err
			}
			*draft = updated
		default:
			ui.InvalidChoice()
			view.Pause()
		}
	}
}

func buildConfig(ports []int, publicWeb, privateWeb bool) (string, error) {
	return buildScopedConfig(ports, true, true, publicWeb, privateWeb)
}

func buildScopedConfig(ports []int, publicSSH, privateSSH, publicWeb, privateWeb bool) (string, error) {
	ports, err := mergeSSHPorts(ports)
	if err != nil {
		return "", err
	}
	sshText := portList(ports)
	publicPorts, publicDescription := scopePortText(publicSSH, sshText, publicWeb)
	privatePorts, privateDescription := scopePortText(privateSSH, sshText, privateWeb)
	content := strings.NewReplacer(
		"__SSH_PORTS__", sshText,
		"__PUBLIC_PORTS__", publicPorts,
		"__PRIVATE_PORTS__", privatePorts,
		"__PUBLIC_PORT_DESCRIPTION__", publicDescription,
		"__PRIVATE_PORT_DESCRIPTION__", privateDescription,
	).Replace(configTemplate)
	if publicPorts == "" {
		content, err = clearScopeElements(content, "public")
		if err != nil {
			return "", err
		}
	}
	if privatePorts == "" {
		content, err = clearScopeElements(content, "private")
		if err != nil {
			return "", err
		}
	}
	return withPingSetting(content, false)
}

func scopePortText(openSSH bool, sshText string, openWeb bool) (string, string) {
	var ports []string
	if openSSH {
		ports = append(ports, sshText)
	}
	if openWeb {
		ports = append(ports, "80, 443")
	}
	switch {
	case openSSH && openWeb:
		return strings.Join(ports, ", "), "当前开放 SSH 端口及 80、443"
	case openSSH:
		return strings.Join(ports, ", "), "当前仅开放 SSH 端口"
	case openWeb:
		return strings.Join(ports, ", "), "当前仅开放 80、443"
	default:
		return "", "当前未开放端口"
	}
}

func clearScopeElements(content, scope string) (string, error) {
	for _, family := range []string{"ipv4", "ipv6"} {
		for _, protocol := range []string{"tcp", "udp"} {
			updated, err := withNamedSetElements(content, "snail_"+scope+"_"+family+"_"+protocol+"_ports", "")
			if err != nil {
				return "", err
			}
			content = updated
		}
	}
	return content, nil
}

func portList(ports []int) string {
	values := make([]string, len(ports))
	for i, port := range ports {
		values[i] = strconv.Itoa(port)
	}
	return strings.Join(values, ", ")
}
