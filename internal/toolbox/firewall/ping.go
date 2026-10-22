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
	for _, value := range []bool{false, true} {
		if sameConfigTokens(body, pingBody(value)) {
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
	block := pingBegin + "\n        " + pingBody(enabled) + "\n        " + pingEnd
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

func configurePing(view *ui.UI) error {
	if !isRoot() {
		return fmt.Errorf("设置 ICMP 回显需要 root 权限，请使用 sudo 运行本工具")
	}
	version, err := readConfigVersion()
	if err != nil {
		return err
	}
	if !version.exists || !hasManagedConfig(string(version.content)) {
		return fmt.Errorf("请先选择 6 生成本工具防火墙配置，再设置 ICMP 回显")
	}
	enabled, found, err := readPingSetting(string(version.content))
	if err != nil {
		return err
	}
	if _, _, _, err := inputChainBounds(string(version.content)); err != nil {
		return err
	}
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "ICMP 回显（Ping）")
	state := "未单独设置（沿用现有规则）"
	if found {
		state = pingLabel(enabled)
	}
	ui.PrintInfoCard("ICMP 回显（Ping）",
		ui.CardField{Label: "配置状态", Value: state},
		ui.CardField{Label: "协议", Value: "ICMP（IPv4） / ICMPv6（IPv6）"},
		ui.CardField{Label: "作用范围", Value: "外部访问本机的回显请求（Echo Request）"},
		ui.CardField{Label: "保留流量", Value: "本机主动 Ping、回环及 IPv6 必需控制报文"},
		ui.CardField{Label: "生效方式", Value: "确认后校验、备份、保存并加载当前完整配置"},
		ui.CardField{Label: "连通条件", Value: "允许仅在本工具内放行，仍受其他规则及云安全组限制"},
	)
	ui.MenuOption("1", "允许 ICMP 回显")
	ui.MenuOption("2", "禁止 ICMP 回显")
	ui.MenuExit("0/q", "返回防火墙管理")
	choice, err := askRuleChoice(view, "请选择", "", map[string]string{"1": "on", "2": "off"})
	if err != nil {
		return err
	}
	target := choice == "on"
	updated, err := withPingSetting(string(version.content), target)
	if err != nil {
		return err
	}
	action := "禁止"
	if target {
		action = "允许"
	}
	confirmed, err := view.Confirm("确认" + action + "外部 ICMP / ICMPv6 回显请求并加载当前配置？(y/N)")
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
	ui.PrintSuccessCard("ICMP 回显配置已保存并加载", ui.CardField{Label: "ICMP / ICMPv6 回显", Value: pingLabel(target)})
	return nil
}
