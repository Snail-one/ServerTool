package firewall

import (
	"errors"
	"fmt"
	"strings"

	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

type sshScopeStatus int

const (
	sshScopeClosed sshScopeStatus = iota
	sshScopeOpen
	sshScopePartial
)

func inputScopeLists(public bool) []policyList {
	var lists []policyList
	for _, list := range policyLists[:8] {
		if strings.Contains(list.name, "_public_") == public {
			lists = append(lists, list)
		}
	}
	return lists
}

func sshScopeStatusOf(content string, lists []policyList, ssh portIntervals) (sshScopeStatus, error) {
	if len(lists) != 4 {
		return sshScopeClosed, fmt.Errorf("SSH 访问名单不完整，请先生成基础配置")
	}
	covered, present := 0, 0
	for _, list := range lists {
		raw, err := readPolicyValue(content, list)
		if err != nil {
			return sshScopeClosed, err
		}
		parsed, err := parsePolicyPorts(raw)
		if err != nil {
			return sshScopeClosed, err
		}
		if len(intersectPorts(parsed, ssh)) > 0 {
			present++
		}
		if len(subtractPorts(ssh, parsed)) == 0 {
			covered++
		}
	}
	switch {
	case covered == len(lists):
		return sshScopeOpen, nil
	case present == 0:
		return sshScopeClosed, nil
	default:
		return sshScopePartial, nil
	}
}

func sshAccessText(status sshScopeStatus) string {
	switch status {
	case sshScopeOpen:
		return "已开放"
	case sshScopePartial:
		return "部分开放"
	default:
		return "已关闭"
	}
}

func sshAccessLabel(status sshScopeStatus) string {
	if status == sshScopeOpen {
		return ui.PrimaryText(sshAccessText(status))
	}
	return sshAccessText(status)
}

// withSSHAccess adds or removes the recorded SSH service ports on one input
// scope. Other ports, the opposite scope, forward lists and snail_ssh_port stay.
func withSSHAccess(content string, public, open bool) (string, bool, error) {
	if _, _, _, err := inputChainBounds(content); err != nil {
		return "", false, err
	}
	ports, err := readSSHPorts(content)
	if err != nil {
		return "", false, err
	}
	ssh, err := parsePolicyPorts(portList(ports))
	if err != nil {
		return "", false, err
	}
	lists := inputScopeLists(public)
	if len(lists) != 4 {
		return "", false, fmt.Errorf("SSH 访问名单不完整，请先生成基础配置")
	}
	type replacement struct{ name, next string }
	var replacements []replacement
	for _, list := range lists {
		current, err := readPolicyValue(content, list)
		if err != nil {
			return "", false, err
		}
		parsed, err := parsePolicyPorts(current)
		if err != nil {
			return "", false, err
		}
		next := subtractPorts(parsed, ssh)
		if open {
			next = unionPorts(append(append(portIntervals(nil), parsed...), ssh...))
		}
		if text := policyPortsText(next); text != current {
			replacements = append(replacements, replacement{list.name, text})
		}
	}
	if len(replacements) == 0 {
		return content, false, nil
	}
	updated := content
	for _, replacement := range replacements {
		updated, err = withNamedSetElements(updated, replacement.name, replacement.next)
		if err != nil {
			return "", false, err
		}
	}
	return updated, true, nil
}

func sshAccessStateField() ui.CardField {
	field := ui.CardField{Label: "SSH 访问"}
	version, err := readConfigVersion()
	if err != nil {
		field.Value = "无法判断"
		field.Detail = err.Error()
		return field
	}
	if !version.exists || !hasManagedConfig(string(version.content)) {
		field.Value = "未配置"
		return field
	}
	content := string(version.content)
	ports, err := readSSHPorts(content)
	if err != nil {
		field.Value = "无法判断"
		field.Detail = err.Error()
		return field
	}
	ssh, err := parsePolicyPorts(portList(ports))
	if err != nil {
		field.Value = "无法判断"
		field.Detail = err.Error()
		return field
	}
	public, err := sshScopeStatusOf(content, inputScopeLists(true), ssh)
	if err != nil {
		field.Value = "无法判断"
		field.Detail = err.Error()
		return field
	}
	private, err := sshScopeStatusOf(content, inputScopeLists(false), ssh)
	if err != nil {
		field.Value = "无法判断"
		field.Detail = err.Error()
		return field
	}
	field.Value = "公网" + sshAccessLabel(public) + "，局域网" + sshAccessLabel(private)
	return field
}

func configureSSHAccess(view *ui.UI) error {
	if !isRoot() {
		return fmt.Errorf("设置 SSH 访问需要 root 权限，请使用 sudo 运行本工具")
	}
	for {
		version, err := readConfigVersion()
		if err != nil {
			return err
		}
		if !version.exists || !hasManagedConfig(string(version.content)) {
			return fmt.Errorf("请先返回防火墙管理，选择 d 生成本工具防火墙配置，再设置 SSH 访问")
		}
		content := string(version.content)
		ports, err := readSSHPorts(content)
		if err != nil {
			return err
		}
		ssh, err := parsePolicyPorts(portList(ports))
		if err != nil {
			return err
		}
		public, err := sshScopeStatusOf(content, inputScopeLists(true), ssh)
		if err != nil {
			return err
		}
		private, err := sshScopeStatusOf(content, inputScopeLists(false), ssh)
		if err != nil {
			return err
		}
		ui.ClearScreen()
		ui.MenuTitle("防火墙管理", "常用功能设置", "SSH 访问")
		ui.PrintInfoCard("SSH 访问",
			ui.CardField{Label: "SSH 端口", Value: portList(ports), Detail: "记录在 set snail_ssh_port，本开关不改服务端口"},
			ui.CardField{Label: "公网", Value: sshAccessLabel(public)},
			ui.CardField{Label: "局域网", Value: sshAccessLabel(private)},
			ui.CardField{Label: "作用范围", Value: "所选范围的 TCP / UDP、IPv4 / IPv6 四份入站名单一起切换"},
			ui.CardField{Label: "不影响", Value: "其他业务端口、转发规则、来源放行，以及 SSH 服务本身"},
		)
		ui.MenuOption("1", "开放公网 SSH")
		ui.MenuOption("2", "关闭公网 SSH")
		ui.MenuOption("3", "开放局域网 SSH")
		ui.MenuOption("4", "关闭局域网 SSH")
		ui.MenuExit("0/q", "返回常用功能设置")
		choice, err := askRuleChoice(view, "请选择", "", map[string]string{
			"1": "public-on", "2": "public-off", "3": "private-on", "4": "private-off",
		})
		if err != nil {
			return err
		}
		publicScope := strings.HasPrefix(choice, "public-")
		open := strings.HasSuffix(choice, "-on")
		scopeName := "局域网"
		current := private
		if publicScope {
			scopeName = "公网"
			current = public
		}
		updated, changed, err := withSSHAccess(content, publicScope, open)
		if err != nil {
			return err
		}
		action := "关闭"
		if open {
			action = "开放"
		}
		if !changed {
			ui.PrintInfoCard("无需修改", ui.CardField{Label: scopeName + " SSH", Value: sshAccessLabel(current)})
			view.Pause()
			continue
		}
		ui.ClearScreen()
		ui.MenuTitle("防火墙管理", "常用功能设置", "SSH 访问", "确认变更")
		ui.PrintInfoCard("即将应用的 SSH 访问设置",
			ui.CardField{Label: "范围", Value: scopeName},
			ui.CardField{Label: "端口", Value: portList(ports)},
			ui.CardField{Label: "当前", Value: sshAccessLabel(current)},
			ui.CardField{Label: "操作", Value: action + scopeName + " SSH 访问"},
			ui.CardField{Label: "生效方式", Value: "保存并加载整份当前配置，包括其他待加载修改"},
		)
		confirmed, err := view.Confirm("确认" + action + scopeName + " SSH 端口 " + portList(ports) + " 并加载当前配置？(y/N，0/q 返回)：")
		if errors.Is(err, shared.ErrReturnToMenu) {
			continue
		}
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
			return fmt.Errorf("SSH 访问配置已保存，但未成功加载；原配置已备份，可输入 r 重试: %w", err)
		}
		ui.PrintSuccessCard(scopeName+" SSH 已"+action,
			ui.CardField{Label: "端口", Value: portList(ports)},
			ui.CardField{Label: "生效状态", Value: "配置已保存并加载"},
		)
		return nil
	}
}
