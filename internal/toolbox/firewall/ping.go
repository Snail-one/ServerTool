package firewall

import (
	"fmt"
	"strings"

	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

const (
	pingBegin = "# ===== BEGIN SNAIL PING ====="
	pingEnd   = "# ===== END SNAIL PING ====="
)

func pingBody(enabled bool) string {
	verdict := "drop"
	if enabled {
		verdict = "accept"
	}
	return fmt.Sprintf("iifname != \"lo\" icmp type echo-request counter %s comment \"snail:ping\"\n        iifname != \"lo\" icmpv6 type echo-request counter %s comment \"snail:ping\"", verdict, verdict)
}

func pingLabel(enabled bool) string {
	if enabled {
		return ui.PrimaryText("已允许")
	}
	return "已禁止"
}

func sameConfigTokens(a, b string) bool {
	x, err := configTokens(a)
	if err != nil {
		return false
	}
	y, err := configTokens(b)
	if err != nil || len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i].text != y[i].text {
			return false
		}
	}
	return true
}

// A Ping block must precede all rules, including established-connection accepts.
// Otherwise an ongoing Ping stream could bypass the disabled setting.
func readPingSetting(content string) (enabled, found bool, err error) {
	if !strings.Contains(content, pingBegin) && !strings.Contains(content, pingEnd) {
		return false, false, nil
	}
	_, close, insertion, err := inputChainBounds(content)
	if err != nil {
		return false, false, err
	}
	begin, end := strings.Index(content, pingBegin), strings.Index(content, pingEnd)
	if strings.Count(content, pingBegin) != 1 || strings.Count(content, pingEnd) != 1 || begin < insertion || end < begin || end+len(pingEnd) > close {
		return false, false, fmt.Errorf("Ping 托管区块标记损坏或位置异常，未修改规则")
	}
	prefix, err := configTokens(content[insertion:begin])
	if err != nil || len(prefix) != 0 {
		return false, false, fmt.Errorf("Ping 区块前存在其他规则，请将 Ping 区块移到 input 链规则最前面")
	}
	body := content[begin+len(pingBegin) : end]
	body, err = withoutCounterStatements(body)
	if err != nil {
		return false, false, err
	}
	for _, value := range []bool{false, true} {
		expected, _ := withoutCounterStatements(pingBody(value))
		if sameConfigTokens(body, expected) {
			return value, true, nil
		}
	}
	return false, false, fmt.Errorf("Ping 托管区块内容已被手动修改，无法确认状态，未覆盖")
}

func withPingSetting(content string, enabled bool) (string, error) {
	_, found, err := readPingSetting(content)
	if err != nil {
		return "", err
	}
	counters, err := packetCountersEnabled(content)
	if err != nil {
		return "", err
	}
	body := pingBody(enabled)
	if !counters {
		body, err = withoutCounterStatements(body)
		if err != nil {
			return "", err
		}
	}
	block := pingBegin + "\n        " + body + "\n        " + pingEnd
	if found {
		begin, end := strings.Index(content, pingBegin), strings.Index(content, pingEnd)
		return content[:begin] + block + content[end+len(pingEnd):], nil
	}
	_, _, insertion, err := inputChainBounds(content)
	if err != nil {
		return "", err
	}
	return content[:insertion] + "\n\n        " + block + content[insertion:], nil
}

func withoutPingSetting(content string) (string, error) {
	_, found, err := readPingSetting(content)
	if err != nil {
		return "", err
	}
	if !found {
		return content, nil
	}
	begin := strings.Index(content, pingBegin)
	end := strings.Index(content, pingEnd) + len(pingEnd)
	return content[:begin] + content[end:], nil
}

func configurePing(view *ui.UI) error {
	if !isRoot() {
		return fmt.Errorf("设置 ICMP 回显需要 root 权限，请使用 sudo 运行本工具")
	}
	version, err := readConfigVersion()
	if err != nil {
		return err
	}
	if !version.exists || !hasManagedConfig(string(version.content)) {
		return fmt.Errorf("请先返回防火墙管理，选择 6 生成本工具防火墙配置，再设置 ICMP 回显")
	}
	enabled, found, err := readPingSetting(string(version.content))
	if err != nil {
		return err
	}
	if _, _, _, err := inputChainBounds(string(version.content)); err != nil {
		return err
	}
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "功能设置", "ICMP 回显（Ping）")
	state := "未设置"
	if found {
		state = pingLabel(enabled)
	}
	installed := commandExists("nft")
	live := liveRules{}
	if installed {
		live = inspectLiveRules()
	}
	fields := echoStateFields(installed, live)
	fields = append(fields, ui.CardField{Label: "保存的开关", Value: state})
	sync := configSyncField(version.content, nil, installed, live)
	sync.Value, _, _ = strings.Cut(sync.Value, " · ")
	if sync.Value == "已同步" || sync.Value == "未同步" {
		sync.Detail = ""
	}
	if sync.Value == "已同步" {
		sync.Value = ui.PrimaryText(sync.Value)
	}
	fields = append(fields, sync)
	ui.PrintInfoCard("ICMP 回显（Ping）", fields...)
	ui.MenuOption("1", "允许 ICMP 回显")
	ui.MenuOption("2", "禁止 ICMP 回显")
	ui.MenuOptionHint("3", "移除回显配置", "删除专用规则，沿用原有规则")
	ui.MenuExit("0/q", "返回功能设置")
	choice, err := askRuleChoice(view, "请选择", "", map[string]string{"1": "on", "2": "off", "3": "remove"})
	if err != nil {
		return err
	}
	target := choice == "on"
	var updated string
	if choice == "remove" {
		if !found {
			ui.PrintInfoCard("无需移除", ui.CardField{Label: "回显配置", Value: "未设置专用规则，当前沿用原有规则"})
			return nil
		}
		updated, err = withoutPingSetting(string(version.content))
	} else {
		updated, err = withPingSetting(string(version.content), target)
	}
	if err != nil {
		return err
	}
	action := "禁止"
	if target {
		action = "允许"
	}
	prompt := "确认" + action + "外部 ICMP / ICMPv6 回显请求并加载当前配置？(y/N)"
	if choice == "remove" {
		ui.PrintInfoCard("移除回显配置",
			ui.CardField{Label: "移除内容", Value: "仅删除专用 ICMP / ICMPv6 回显规则及其标记"},
			ui.CardField{Label: "移除后", Value: "由原有规则决定，项目默认规则通常仍拦截外部 Ping"},
		)
		prompt = "确认移除专用回显配置并加载当前配置？(y/N)"
	}
	confirmed, err := view.Confirm(prompt)
	if err != nil {
		return err
	}
	if !confirmed {
		return shared.ErrReturnToMenu
	}
	if err := writeConfig(updated, version); err != nil {
		return err
	}
	if err := applyConfig([]byte(updated)); err != nil {
		return fmt.Errorf("ICMP 回显配置已保存，但未成功加载；原配置已备份，可输入 r 重试: %w", err)
	}
	if choice == "remove" {
		ui.PrintSuccessCard("回显配置已移除并加载", ui.CardField{Label: "保存的开关", Value: "未设置，沿用原有规则"})
	} else {
		ui.PrintSuccessCard("ICMP 回显配置已保存并加载", ui.CardField{Label: "ICMP / ICMPv6 回显", Value: pingLabel(target)})
	}
	return nil
}
