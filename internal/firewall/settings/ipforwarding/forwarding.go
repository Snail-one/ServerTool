package ipforwarding

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"snail_tool/internal/storage"
	"snail_tool/internal/ui"
)

const (
	forwardingBegin = "# ===== BEGIN SNAIL IP FORWARDING ====="
	forwardingEnd   = "# ===== END SNAIL IP FORWARDING ====="
	ipv4Forwarding  = "net.ipv4.ip_forward"
	ipv6Forwarding  = "net.ipv6.conf.all.forwarding"
)

const (
	DefaultProcRoot   = "/proc/sys"
	DefaultConfigPath = "/etc/sysctl.d/99-snail-forwarding.conf"
)

type Service struct {
	ProcRoot, ConfigPath string
	IsRoot               func() bool
	CommandExists        func(string) bool
	CommandOutput        func(string, ...string) (string, error)
}

func (service Service) StatusField() ui.CardField {
	return forwardingStateField(service.inspect())
}

type forwardingState struct {
	family      string
	key         string
	value       string
	unavailable bool
	err         error
}

// Read the running kernel directly; nftables availability and saved sysctl
// configuration do not imply anything about the actual forwarding state.
func (service Service) inspect() []forwardingState {
	states := []forwardingState{
		{family: "IPv4", key: ipv4Forwarding},
		{family: "IPv6", key: ipv6Forwarding},
	}
	for i := range states {
		state := &states[i]
		path := filepath.Join(service.ProcRoot, strings.ReplaceAll(state.key, ".", "/"))
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			state.unavailable = true
			continue
		}
		state.value, state.err = strings.TrimSpace(string(data)), err
		if err == nil && state.value != "0" && state.value != "1" {
			state.err = fmt.Errorf("%s 返回无效值 %q", state.key, state.value)
		}
	}
	return states
}

func forwardingStateField(states []forwardingState) ui.CardField {
	field := ui.CardField{Label: "内核转发"}
	var labels, details []string
	for _, state := range states {
		label := "已关闭"
		switch {
		case state.unavailable:
			label = "不支持"
		case state.err != nil:
			label = "检测失败"
			details = append(details, state.family+": "+state.err.Error())
		case state.value == "1":
			label = ui.PrimaryText("已开启")
		}
		labels = append(labels, state.family+" "+label)
	}
	field.Value, field.Detail = strings.Join(labels, " · "), strings.Join(details, "；")
	return field
}

func validateForwarding(states []forwardingState) error {
	available := false
	for _, state := range states {
		if state.err != nil {
			return fmt.Errorf("读取 %s 内核转发状态失败，未修改: %w", state.family, state.err)
		}
		available = available || !state.unavailable
	}
	if !available {
		return fmt.Errorf("当前内核未提供所选协议的转发开关")
	}
	return nil
}

// Keep the other protocol's saved setting independently of its runtime state.
// Both enabled and disabled settings are persisted until explicitly removed.
func updateForwardingConfig(previous []byte, key string, enabled bool) ([]byte, error) {
	content := string(previous)
	cleaned, err := storage.RemoveManagedBlockChecked(content, forwardingBegin, forwardingEnd)
	if err != nil {
		return nil, err
	}
	saved := make(map[string]bool)
	if body, found := storage.ManagedBlockContent(content, forwardingBegin, forwardingEnd); found {
		for _, line := range strings.Split(body, "\n") {
			line, _, _ = strings.Cut(line, "#")
			if strings.TrimSpace(line) == "" {
				continue
			}
			name, value, ok := strings.Cut(line, "=")
			name, value = strings.TrimSpace(name), strings.TrimSpace(value)
			if !ok || (name != ipv4Forwarding && name != ipv6Forwarding) || (value != "0" && value != "1") {
				return nil, fmt.Errorf("内核转发托管区块内容异常，未修改")
			}
			if _, duplicate := saved[name]; duplicate {
				return nil, fmt.Errorf("内核转发托管区块含重复参数 %s，未修改", name)
			}
			saved[name] = value == "1"
		}
	}
	saved[key] = enabled
	var settings []string
	for _, name := range []string{ipv4Forwarding, ipv6Forwarding} {
		if value, found := saved[name]; found {
			setting := "0"
			if value {
				setting = "1"
			}
			settings = append(settings, name+" = "+setting)
		}
	}
	return []byte(storage.AppendBlock(cleaned, storage.FormatManagedBlock(forwardingBegin, strings.Join(settings, "\n"), forwardingEnd))), nil
}

func (service Service) Run(view *ui.UI) error {
	if !service.IsRoot() {
		return fmt.Errorf("设置内核转发需要 root 权限，请使用 sudo 运行本工具")
	}
	states := service.inspect()
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "常用功能设置", "内核转发")
	ui.PrintInfoCard("内核转发", forwardingStateField(states))
	for index, state := range states {
		available := state.err == nil && !state.unavailable
		ui.MenuOptionStatusHint(fmt.Sprint(index*2+1), "开启 "+state.family+" 转发", ui.CurrentBadge(available && state.value == "1"), "持久化写入 1，并立即开启")
		ui.MenuOptionStatusHint(fmt.Sprint(index*2+2), "关闭 "+state.family+" 转发", ui.CurrentBadge(available && state.value == "0"), "持久化写入 0，并立即关闭")
	}
	ui.MenuOptionHint("5", "恢复默认", "删除配置文件，同时关闭 IPv4 / IPv6 转发")
	ui.MenuOptionHint("6", "仅删除配置文件", "移除持久配置，当前转发状态保持不变")
	ui.MenuExit("0/q", "返回常用功能设置")
	choice, err := ui.AskChoice(view, "请选择", "", map[string]string{"1": "ipv4-on", "2": "ipv4-off", "3": "ipv6-on", "4": "ipv6-off", "5": "reset", "6": "remove-config"})
	if err != nil {
		return err
	}
	index := 0
	if strings.HasPrefix(choice, "ipv6-") {
		index = 1
	}
	state := states[index]
	reset := choice == "reset"
	removeOnly := choice == "remove-config"
	selected := states[index : index+1]
	if reset {
		selected = states
	}
	if !removeOnly {
		if err := validateForwarding(selected); err != nil {
			return err
		}
		if !service.CommandExists("sysctl") {
			return fmt.Errorf("未找到 sysctl，无法设置内核转发")
		}
	}
	previous, exists, err := storage.ReadRegularFile(service.ConfigPath)
	if err != nil {
		return err
	}
	enabled := strings.HasSuffix(choice, "-on")
	var updated []byte
	if !reset && !removeOnly {
		updated, err = updateForwardingConfig(previous, state.key, enabled)
		if err != nil {
			return err
		}
	}
	target, action := "0", "关闭"
	if enabled {
		target, action = "1", "开启"
	}
	configAction := "直接删除整个文件 " + service.ConfigPath + "；重启后由系统其他配置及服务决定转发状态"
	configResult := "内核转发持久配置已移除"
	if updated != nil {
		configAction = "持久化写入 " + state.key + " = " + target + " 至 " + service.ConfigPath
		configResult = "内核转发配置已保存"
	}
	networkBehavior := "切换 IPv4 转发会重置相关内核网络参数"
	if index == 1 {
		networkBehavior = "开启 IPv6 转发可能影响 RA 自动配置"
	}
	operation := action + state.family + " 转发"
	successTitle := state.family + " 转发已" + action
	if reset {
		operation = "恢复默认：删除配置文件，同时关闭 IPv4 / IPv6 转发（不支持的协议跳过）"
		successTitle = "内核转发已恢复默认"
	}
	fields := []ui.CardField{{Label: "操作", Value: operation}}
	if removeOnly {
		operation = "仅删除内核转发配置文件"
		fields[0].Value = operation
		fields = append(fields, ui.CardField{Label: "生效说明", Value: "当前 IPv4 / IPv6 转发状态保持不变；重启后由系统其他配置及服务决定"})
	} else {
		fields = append(fields,
			ui.CardField{Label: "影响", Value: "关闭会影响 Docker、VPN 和路由转发；开启后仍受转发规则约束"},
			ui.CardField{Label: "网络行为", Value: networkBehavior},
		)
	}
	fields = append(fields, ui.CardField{Label: "持久配置", Value: configAction})
	ui.PrintWarningCard("确认内核转发设置", fields...)
	prompt := "是否" + operation + "？(y/N，0/q 返回)："
	if !removeOnly && !enabled && (reset || index == 0) && !states[0].unavailable && (service.CommandExists("docker") || service.CommandExists("dockerd")) {
		prompt = "检测到 Docker，Docker 需要启用 IPv4 转发，确认关闭吗？(y/N，0/q 返回)："
		if reset {
			prompt = "检测到 Docker，Docker 需要启用 IPv4 转发，确认恢复默认并关闭 IPv4 / IPv6 转发吗？(y/N，0/q 返回)："
		}
	}
	confirmed, err := view.Confirm(prompt)
	if err != nil {
		return err
	}
	if !confirmed {
		return ui.ErrReturnToMenu
	}
	if !removeOnly {
		latest := service.inspect()
		if !reset {
			latest = latest[index : index+1]
		}
		if err := validateForwarding(latest); err != nil {
			return err
		}
		for i, expected := range selected {
			if expected.value != latest[i].value || expected.unavailable != latest[i].unavailable {
				return fmt.Errorf("内核转发状态在确认期间发生变化，未修改，请重新操作")
			}
		}
	}
	current, currentExists, err := storage.ReadRegularFile(service.ConfigPath)
	if err != nil {
		return err
	}
	if currentExists != exists || !bytes.Equal(current, previous) {
		return fmt.Errorf("内核转发配置在确认期间发生变化，未修改，请重新操作")
	}
	if (updated == nil && exists) || (updated != nil && !bytes.Equal(previous, updated)) {
		if updated == nil {
			if err := storage.RemoveRegularFile(service.ConfigPath); err != nil {
				return fmt.Errorf("删除内核转发配置失败，尚未修改运行状态: %w", err)
			}
		} else {
			if err := storage.AtomicWriteFile(service.ConfigPath, updated, storage.AtomicWriteOptions{Mode: 0644}); err != nil {
				return fmt.Errorf("保存内核转发配置失败，尚未修改运行状态: %w", err)
			}
		}
	}
	if removeOnly {
		ui.PrintSuccessCard("内核转发持久配置已移除",
			ui.CardField{Label: "配置文件", Value: service.ConfigPath},
			ui.CardField{Label: "当前转发", Value: "保持不变，未修改内核参数"},
		)
		return nil
	}
	// Apply only these keys, never sysctl --system or -p: unrelated pending
	// settings must not be loaded. Skip unchanged values to avoid IPv4 resets.
	var failures []string
	for _, expected := range selected {
		if expected.unavailable || expected.value == target {
			continue
		}
		out, err := service.CommandOutput("sysctl", "-w", expected.key+"="+target)
		if err != nil {
			failures = append(failures, fmt.Sprintf("设置 %s 失败: %v: %s", expected.family, err, strings.TrimSpace(out)))
		}
	}
	result := service.inspect()
	applied := result[index : index+1]
	if reset {
		applied = result
	}
	for i, current := range applied {
		if selected[i].unavailable {
			continue
		}
		if current.err != nil || current.unavailable || current.value != target {
			failures = append(failures, fmt.Sprintf("无法确认 %s 已%s", current.family, action))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s，但转发设置未全部完成，请刷新检查并重试：%s", configResult, strings.Join(failures, "；"))
	}
	ui.PrintSuccessCard(successTitle, forwardingStateField(result),
		ui.CardField{Label: "持久配置", Value: configResult + "：" + service.ConfigPath},
	)
	return nil
}
