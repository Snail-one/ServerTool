package firewall

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

const (
	forwardingBegin = "# ===== BEGIN SNAIL IP FORWARDING ====="
	forwardingEnd   = "# ===== END SNAIL IP FORWARDING ====="
	ipv4Forwarding  = "net.ipv4.ip_forward"
	ipv6Forwarding  = "net.ipv6.conf.all.forwarding"
)

var (
	forwardingProcRoot   = "/proc/sys"
	forwardingConfigPath = "/etc/sysctl.d/99-snail-forwarding.conf"
)

type forwardingState struct {
	family      string
	key         string
	value       string
	unavailable bool
	err         error
}

// Read the running kernel directly; nftables availability and saved sysctl
// configuration do not imply anything about the actual forwarding state.
func inspectForwarding() []forwardingState {
	states := []forwardingState{
		{family: "IPv4", key: ipv4Forwarding},
		{family: "IPv6", key: ipv6Forwarding},
	}
	for i := range states {
		state := &states[i]
		path := filepath.Join(forwardingProcRoot, strings.ReplaceAll(state.key, ".", "/"))
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
// A nil result removes the file when neither protocol has a saved enabled value.
func updateForwardingConfig(previous []byte, key string, enabled bool) ([]byte, error) {
	content := string(previous)
	cleaned, err := shared.RemoveManagedBlockChecked(content, forwardingBegin, forwardingEnd)
	if err != nil {
		return nil, err
	}
	saved := make(map[string]bool)
	if body, found := shared.ManagedBlockContent(content, forwardingBegin, forwardingEnd); found {
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
		if saved[name] {
			settings = append(settings, name+" = 1")
		}
	}
	if len(settings) == 0 {
		return nil, nil
	}
	return []byte(shared.AppendBlock(cleaned, shared.FormatManagedBlock(forwardingBegin, strings.Join(settings, "\n"), forwardingEnd))), nil
}

func configureForwarding(view *ui.UI) error {
	if !isRoot() {
		return fmt.Errorf("设置内核转发需要 root 权限，请使用 sudo 运行本工具")
	}
	states := inspectForwarding()
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "常用功能设置", "内核转发")
	ui.PrintInfoCard("内核转发", forwardingStateField(states),
		ui.CardField{Label: "作用范围", Value: "分别设置 IPv4 / IPv6 全局转发"},
		ui.CardField{Label: "生效方式", Value: "立即生效；开启保存对应配置，关闭移除对应配置，两者均无持久开启配置时删除文件"},
		ui.CardField{Label: "配置文件", Value: forwardingConfigPath},
	)
	ui.MenuOptionHint("1", "开启 IPv4 转发", "保存 IPv4 持久配置并立即开启")
	ui.MenuOptionHint("2", "关闭 IPv4 转发", "移除 IPv4 持久配置并立即关闭")
	ui.MenuOptionHint("3", "开启 IPv6 转发", "保存 IPv6 持久配置并立即开启")
	ui.MenuOptionHint("4", "关闭 IPv6 转发", "移除 IPv6 持久配置并立即关闭")
	ui.MenuOptionHint("5", "恢复默认", "删除配置文件，同时关闭 IPv4 / IPv6 转发")
	ui.MenuExit("0/q", "返回常用功能设置")
	choice, err := askRuleChoice(view, "请选择", "", map[string]string{"1": "ipv4-on", "2": "ipv4-off", "3": "ipv6-on", "4": "ipv6-off", "5": "reset"})
	if err != nil {
		return err
	}
	index := 0
	if strings.HasPrefix(choice, "ipv6-") {
		index = 1
	}
	state := states[index]
	reset := choice == "reset"
	selected := states[index : index+1]
	if reset {
		selected = states
	}
	if err := validateForwarding(selected); err != nil {
		return err
	}
	if !commandExists("sysctl") {
		return fmt.Errorf("未找到 sysctl，无法设置内核转发")
	}
	previous, exists, err := shared.ReadRegularFile(forwardingConfigPath)
	if err != nil {
		return err
	}
	enabled := strings.HasSuffix(choice, "-on")
	var updated []byte
	if !reset {
		updated, err = updateForwardingConfig(previous, state.key, enabled)
		if err != nil {
			return err
		}
	}
	target, action := "0", "关闭"
	if enabled {
		target, action = "1", "开启"
	}
	configAction := "直接删除整个文件 " + forwardingConfigPath + "；重启后由系统其他配置及服务决定转发状态"
	configResult := "内核转发持久配置已移除"
	if updated != nil {
		configAction = "保存至 " + forwardingConfigPath + "（开机加载时可能被其他系统配置覆盖）"
		configResult = "内核转发配置已保存"
		if !enabled {
			configAction = "移除 " + state.family + " 持久配置，保留另一协议的开启配置；重启后由系统其他配置及服务决定该协议的转发状态"
			configResult = state.family + " 持久配置已移除，另一协议的配置已保留"
		}
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
	ui.PrintWarningCard("确认内核转发设置",
		ui.CardField{Label: "操作", Value: operation},
		ui.CardField{Label: "影响", Value: "关闭会影响 Docker、VPN 和路由转发；开启后仍受转发规则约束"},
		ui.CardField{Label: "网络行为", Value: networkBehavior},
		ui.CardField{Label: "持久配置", Value: configAction},
	)
	confirmed, err := view.Confirm("是否" + operation + "？(y/N，0/q 返回)：")
	if err != nil {
		return err
	}
	if !confirmed {
		return shared.ErrReturnToMenu
	}
	latest := inspectForwarding()
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
	current, currentExists, err := shared.ReadRegularFile(forwardingConfigPath)
	if err != nil {
		return err
	}
	if currentExists != exists || !bytes.Equal(current, previous) {
		return fmt.Errorf("内核转发配置在确认期间发生变化，未修改，请重新操作")
	}
	if (updated == nil && exists) || (updated != nil && !bytes.Equal(previous, updated)) {
		if updated == nil {
			if err := shared.RemoveRegularFile(forwardingConfigPath); err != nil {
				return fmt.Errorf("删除内核转发配置失败，尚未修改运行状态: %w", err)
			}
		} else {
			if err := shared.AtomicWriteFile(forwardingConfigPath, updated, shared.AtomicWriteOptions{Mode: 0644}); err != nil {
				return fmt.Errorf("保存内核转发配置失败，尚未修改运行状态: %w", err)
			}
		}
	}
	// Apply only these keys, never sysctl --system or -p: unrelated pending
	// settings must not be loaded. Skip unchanged values to avoid IPv4 resets.
	var failures []string
	for _, expected := range selected {
		if expected.unavailable || expected.value == target {
			continue
		}
		out, err := commandOutput("sysctl", "-w", expected.key+"="+target)
		if err != nil {
			failures = append(failures, fmt.Sprintf("设置 %s 失败: %v: %s", expected.family, err, strings.TrimSpace(out)))
		}
	}
	result := inspectForwarding()
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
		ui.CardField{Label: "持久配置", Value: configResult + "：" + forwardingConfigPath},
	)
	return nil
}
