package sshaccess

import (
	"errors"
	"fmt"
	"strings"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/ui"
)

type sshScopeStatus int

const (
	ScopeClosed sshScopeStatus = iota
	ScopeOpen
	ScopePartial
)

func ScopeStatusOf(content string, public bool, ssh configuration.PortIntervals) (sshScopeStatus, error) {
	lists := configuration.InputPortSets(public)
	covered, present := 0, 0
	for _, list := range lists {
		raw, err := configuration.ReadPortSet(content, list)
		if err != nil {
			return ScopeClosed, err
		}
		parsed, err := configuration.ParsePorts(raw)
		if err != nil {
			return ScopeClosed, err
		}
		if len(configuration.IntersectPorts(parsed, ssh)) > 0 {
			present++
		}
		if len(configuration.SubtractPorts(ssh, parsed)) == 0 {
			covered++
		}
	}
	switch {
	case covered == len(lists):
		return ScopeOpen, nil
	case present == 0:
		return ScopeClosed, nil
	default:
		return ScopePartial, nil
	}
}

func ScopeText(status sshScopeStatus) string {
	switch status {
	case ScopeOpen:
		return "已开放"
	case ScopePartial:
		return "部分开放"
	default:
		return "已关闭"
	}
}

func sshAccessLabel(status sshScopeStatus) string {
	return ui.StateText(ScopeText(status), status == ScopeOpen)
}

// ReplaceAccess adds or removes the recorded SSH service ports on one input
// scope. Other ports, the opposite scope, forward lists and snail_ssh_port stay.
func ReplaceAccess(content string, public, open bool) (string, bool, error) {
	if _, _, _, err := configuration.InputChainBounds(content); err != nil {
		return "", false, err
	}
	ports, err := ReadPorts(content)
	if err != nil {
		return "", false, err
	}
	ssh, err := configuration.ParsePorts(PortList(ports))
	if err != nil {
		return "", false, err
	}
	lists := configuration.InputPortSets(public)
	type replacement struct{ name, next string }
	var replacements []replacement
	for _, list := range lists {
		current, err := configuration.ReadPortSet(content, list)
		if err != nil {
			return "", false, err
		}
		parsed, err := configuration.ParsePorts(current)
		if err != nil {
			return "", false, err
		}
		next := configuration.SubtractPorts(parsed, ssh)
		if open {
			next = configuration.UnionPorts(append(append(configuration.PortIntervals(nil), parsed...), ssh...))
		}
		if text := configuration.PortsText(next); text != current {
			replacements = append(replacements, replacement{list.Name, text})
		}
	}
	if len(replacements) == 0 {
		return content, false, nil
	}
	updated := content
	for _, replacement := range replacements {
		updated, err = configuration.ReplaceNamedSetElements(updated, replacement.name, replacement.next)
		if err != nil {
			return "", false, err
		}
	}
	return updated, true, nil
}

func (service Service) StateField() ui.CardField {
	field := ui.CardField{Label: "SSH 访问"}
	version, err := service.Store.Read()
	if err != nil {
		field.Value = "无法判断"
		field.Detail = err.Error()
		return field
	}
	if !version.Exists || !service.HasManagedConfig(string(version.Content)) {
		field.Value = "未配置"
		return field
	}
	content := string(version.Content)
	ports, err := ReadPorts(content)
	if err != nil {
		field.Value = "无法判断"
		field.Detail = err.Error()
		return field
	}
	ssh, err := configuration.ParsePorts(PortList(ports))
	if err != nil {
		field.Value = "无法判断"
		field.Detail = err.Error()
		return field
	}
	public, err := ScopeStatusOf(content, true, ssh)
	if err != nil {
		field.Value = "无法判断"
		field.Detail = err.Error()
		return field
	}
	private, err := ScopeStatusOf(content, false, ssh)
	if err != nil {
		field.Value = "无法判断"
		field.Detail = err.Error()
		return field
	}
	field.Value = "公网" + sshAccessLabel(public) + "，局域网" + sshAccessLabel(private)
	return field
}

func (service Service) Run(view *ui.UI) error {
	if !service.IsRoot() {
		return fmt.Errorf("设置 SSH 访问需要 root 权限，请使用 sudo 运行本工具")
	}
	for {
		version, err := service.Store.Read()
		if err != nil {
			return err
		}
		if !version.Exists || !service.HasManagedConfig(string(version.Content)) {
			return fmt.Errorf("请先返回防火墙管理，选择 d 生成本工具防火墙配置，再设置 SSH 访问")
		}
		content := string(version.Content)
		ports, err := ReadPorts(content)
		if err != nil {
			return err
		}
		ssh, err := configuration.ParsePorts(PortList(ports))
		if err != nil {
			return err
		}
		public, err := ScopeStatusOf(content, true, ssh)
		if err != nil {
			return err
		}
		private, err := ScopeStatusOf(content, false, ssh)
		if err != nil {
			return err
		}
		choice, err := Choose(view, PortList(ports), public, private)
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
		updated, changed, err := ReplaceAccess(content, publicScope, open)
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
			ui.CardField{Label: "端口", Value: PortList(ports)},
			ui.CardField{Label: "当前", Value: sshAccessLabel(current)},
			ui.CardField{Label: "操作", Value: action + scopeName + " SSH 访问", Changed: true},
			ui.CardField{Label: "生效方式", Value: "保存并加载整份当前配置，包括其他待加载修改"},
		)
		confirmed, err := service.Confirm(view, "确认"+action+scopeName+" SSH 端口 "+PortList(ports)+" 并加载当前配置？")
		if errors.Is(err, ui.ErrReturnToMenu) {
			continue
		}
		if err != nil {
			return err
		}
		if !confirmed {
			return ui.ErrReturnToMenu
		}
		saved, err := service.Store.SaveAndApply(version, updated)
		if err != nil {
			if !saved {
				return err
			}
			return fmt.Errorf("SSH 访问配置已保存，但未成功加载；原配置已备份，可输入 r 重试: %w", err)
		}
		ui.PrintSuccessCard(scopeName+" SSH 已"+action,
			ui.CardField{Label: "端口", Value: PortList(ports)},
			ui.CardField{Label: "生效状态", Value: "配置已保存并加载"},
		)
		return nil
	}
}
