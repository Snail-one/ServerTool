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
		{family: "IPv4", key: "net.ipv4.ip_forward"},
		{family: "IPv6", key: "net.ipv6.conf.all.forwarding"},
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
		return fmt.Errorf("当前内核未提供 IPv4 / IPv6 转发开关")
	}
	return nil
}

func configureForwarding(view *ui.UI) error {
	if !isRoot() {
		return fmt.Errorf("设置内核转发需要 root 权限，请使用 sudo 运行本工具")
	}
	states := inspectForwarding()
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "常用功能设置", "内核转发")
	ui.PrintInfoCard("内核转发", forwardingStateField(states),
		ui.CardField{Label: "作用范围", Value: "同时设置内核支持的 IPv4 / IPv6 全局转发"},
		ui.CardField{Label: "生效方式", Value: "立即生效；开启时保存配置，关闭时直接删除配置文件"},
		ui.CardField{Label: "配置文件", Value: forwardingConfigPath},
	)
	ui.MenuOptionHint("1", "开启内核转发", "保存持久配置并立即开启")
	ui.MenuOptionHint("2", "关闭内核转发", "直接删除配置文件，立即关闭")
	ui.MenuExit("0/q", "返回常用功能设置")
	choice, err := askRuleChoice(view, "请选择", "", map[string]string{"1": "on", "2": "off"})
	if err != nil {
		return err
	}
	if err := validateForwarding(states); err != nil {
		return err
	}
	if !commandExists("sysctl") {
		return fmt.Errorf("未找到 sysctl，无法设置内核转发")
	}
	previous, exists, err := shared.ReadRegularFile(forwardingConfigPath)
	if err != nil {
		return err
	}
	// Own a dedicated block, retaining any additional content in this file.
	cleaned, err := shared.RemoveManagedBlockChecked(string(previous), forwardingBegin, forwardingEnd)
	if err != nil {
		return err
	}
	target, action := "0", "关闭"
	if choice == "on" {
		target, action = "1", "开启"
	}
	var updated []byte
	configAction := "直接删除整个文件 " + forwardingConfigPath + "；重启后由系统其他配置及服务决定转发状态"
	configResult := "内核转发持久配置已移除"
	if choice == "on" {
		var settings []string
		for _, state := range states {
			if !state.unavailable {
				settings = append(settings, state.key+" = "+target)
			}
		}
		updated = []byte(shared.AppendBlock(cleaned, shared.FormatManagedBlock(forwardingBegin, strings.Join(settings, "\n"), forwardingEnd)))
		configAction = "保存至 " + forwardingConfigPath + "（开机加载时可能被其他系统配置覆盖）"
		configResult = "内核转发配置已保存"
	}
	ui.PrintWarningCard("确认内核转发设置",
		ui.CardField{Label: "操作", Value: action + "内核支持的 IPv4 / IPv6 转发"},
		ui.CardField{Label: "影响", Value: "关闭会影响 Docker、VPN 和路由转发；开启后仍受转发规则约束"},
		ui.CardField{Label: "网络行为", Value: "切换 IPv4 转发会重置相关内核网络参数；开启 IPv6 转发可能影响 RA 自动配置"},
		ui.CardField{Label: "持久配置", Value: configAction},
	)
	confirmed, err := view.Confirm("是否" + action + "内核转发？(y/N，0/q 返回)：")
	if err != nil {
		return err
	}
	if !confirmed {
		return shared.ErrReturnToMenu
	}
	latest := inspectForwarding()
	if err := validateForwarding(latest); err != nil {
		return err
	}
	for i, state := range states {
		if state.value != latest[i].value || state.unavailable != latest[i].unavailable {
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
	if (choice == "off" && exists) || (choice == "on" && !bytes.Equal(previous, updated)) {
		if choice == "off" {
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
	for _, state := range states {
		if state.unavailable || state.value == target {
			continue
		}
		out, err := commandOutput("sysctl", "-w", state.key+"="+target)
		if err != nil {
			return fmt.Errorf("%s，但设置 %s 失败，运行状态可能部分生效，请刷新检查并重试: %w: %s", configResult, state.family, err, strings.TrimSpace(out))
		}
	}
	result := inspectForwarding()
	for i, state := range result {
		if states[i].unavailable {
			continue
		}
		if state.err != nil || state.unavailable || state.value != target {
			return fmt.Errorf("%s，但无法确认 %s 已%s，请刷新检查并重试", configResult, state.family, action)
		}
	}
	ui.PrintSuccessCard("内核转发已"+action, forwardingStateField(result),
		ui.CardField{Label: "持久配置", Value: configResult + "：" + forwardingConfigPath},
	)
	return nil
}
