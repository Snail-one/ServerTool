package ping

import (
	"errors"
	"fmt"
	"strings"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/firewall/settings/counters"
	"snail_tool/internal/ui"
)

type Service struct {
	Store            configuration.Store
	IsRoot           func() bool
	HasManagedConfig func(string) bool
	CommandExists    func(string) bool
	InspectLive      func() (json string, inputActive bool, err error)
}

const (
	Begin = "# ===== BEGIN SNAIL PING ====="
	End   = "# ===== END SNAIL PING ====="
)

func pingBody(enabled bool) string {
	verdict := "drop"
	if enabled {
		verdict = "accept"
	}
	return fmt.Sprintf("iifname != \"lo\" icmp type echo-request counter %s comment \"snail:ping\"\n        iifname != \"lo\" icmpv6 type echo-request counter %s comment \"snail:ping\"", verdict, verdict)
}

func Label(enabled bool) string {
	if enabled {
		return ui.PrimaryText("已允许")
	}
	return "已禁止"
}

func Read(content string) (enabled, found bool, err error) {
	if !strings.Contains(content, Begin) && !strings.Contains(content, End) {
		return false, false, nil
	}
	_, close, insertion, err := configuration.InputChainBounds(content)
	if err != nil {
		return false, false, err
	}
	begin, end := strings.Index(content, Begin), strings.Index(content, End)
	if strings.Count(content, Begin) != 1 || strings.Count(content, End) != 1 || begin < insertion || end < begin || end+len(End) > close {
		return false, false, fmt.Errorf("Ping 托管区块标记损坏或位置异常，未修改规则")
	}
	prefix, err := configuration.Tokens(content[insertion:begin])
	if err != nil || len(prefix) != 0 {
		return false, false, fmt.Errorf("Ping 区块前存在其他规则，请将 Ping 区块移到 input 链规则最前面")
	}
	body := content[begin+len(Begin) : end]
	body, err = counters.WithoutStatements(body)
	if err != nil {
		return false, false, err
	}
	for _, value := range []bool{false, true} {
		expected, _ := counters.WithoutStatements(pingBody(value))
		if configuration.SameTokens(body, expected) {
			return value, true, nil
		}
	}
	return false, false, fmt.Errorf("Ping 托管区块内容已被手动修改，无法确认状态，未覆盖")
}

func Replace(content string, enabled bool) (string, error) {
	_, found, err := Read(content)
	if err != nil {
		return "", err
	}
	countPackets, err := counters.Enabled(content)
	if err != nil {
		return "", err
	}
	body := pingBody(enabled)
	if !countPackets {
		body, err = counters.WithoutStatements(body)
		if err != nil {
			return "", err
		}
	}
	block := Begin + "\n        " + body + "\n        " + End
	if found {
		begin, end := strings.Index(content, Begin), strings.Index(content, End)
		return content[:begin] + block + content[end+len(End):], nil
	}
	_, _, insertion, err := configuration.InputChainBounds(content)
	if err != nil {
		return "", err
	}
	return content[:insertion] + "\n\n        " + block + content[insertion:], nil
}

func Remove(content string) (string, error) {
	_, found, err := Read(content)
	if err != nil {
		return "", err
	}
	if !found {
		return content, nil
	}
	begin := strings.Index(content, Begin)
	end := strings.Index(content, End) + len(End)
	return content[:begin] + content[end:], nil
}

func (service Service) Run(view *ui.UI) error {
	if !service.IsRoot() {
		return fmt.Errorf("设置 ICMP 回显需要 root 权限，请使用 sudo 运行本工具")
	}
	version, err := service.Store.Read()
	if err != nil {
		return err
	}
	if !version.Exists || !service.HasManagedConfig(string(version.Content)) {
		return fmt.Errorf("请先返回防火墙管理，选择 d 生成本工具防火墙配置，再设置 ICMP 回显")
	}
	enabled, found, err := Read(string(version.Content))
	if err != nil {
		return err
	}
	if _, _, _, err := configuration.InputChainBounds(string(version.Content)); err != nil {
		return err
	}
	for {
		state := "未设置"
		if found {
			state = Label(enabled)
		}
		installed := service.CommandExists("nft")
		var liveJSON string
		var liveErr error
		inputActive := false
		if installed {
			liveJSON, inputActive, liveErr = service.InspectLive()
		}
		fields := StateFields(installed, inputActive, liveJSON, liveErr)
		fields = append(fields, ui.CardField{Label: "保存的开关", Value: state})
		sync := service.Store.SyncField(version.Content, nil, installed, liveJSON, liveErr, service.CommandExists)
		sync.Value, _, _ = strings.Cut(sync.Value, " · ")
		if sync.Value == "已同步" || sync.Value == "未同步" {
			sync.Detail = ""
		}
		if sync.Value == "已同步" {
			sync.Value = ui.PrimaryText(sync.Value)
		}
		fields = append(fields, sync)
		choice, err := Choose(view, fields, enabled, found)
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
			updated, err = Remove(string(version.Content))
		} else {
			updated, err = Replace(string(version.Content), target)
		}
		if err != nil {
			return err
		}
		action := "禁止"
		if target {
			action = "允许"
		}
		ui.ClearScreen()
		ui.MenuTitle("防火墙管理", "常用功能设置", "ICMP 回显（Ping）", "确认变更")
		operation := action + "外部 ICMP / ICMPv6 回显请求"
		if choice == "remove" {
			operation = "移除专用回显配置"
		}
		ui.PrintInfoCard("即将应用的回显设置",
			ui.CardField{Label: "当前配置", Value: state},
			ui.CardField{Label: "操作", Value: operation, Changed: updated != string(version.Content)},
			ui.CardField{Label: "生效方式", Value: "保存并加载整份当前配置，包括其他待加载修改"},
		)
		prompt := "确认" + action + "外部 ICMP / ICMPv6 回显请求并加载当前配置？"
		if choice == "remove" {
			ui.PrintInfoCard("移除回显配置",
				ui.CardField{Label: "移除内容", Value: "仅删除专用 ICMP / ICMPv6 回显规则及其标记"},
				ui.CardField{Label: "移除后", Value: "由原有规则决定，项目默认规则通常仍拦截外部 Ping", Changed: true},
			)
			prompt = "确认移除专用回显配置并加载当前配置？"
		}
		confirmed, err := ui.ConfirmMenuChange(view, prompt, "常用功能设置")
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
			return fmt.Errorf("ICMP 回显配置已保存，但未成功加载；原配置已备份，可输入 r 重试: %w", err)
		}
		if choice == "remove" {
			ui.PrintSuccessCard("回显配置已移除并加载", ui.CardField{Label: "保存的开关", Value: "未设置，沿用原有规则"})
		} else {
			ui.PrintSuccessCard("ICMP 回显配置已保存并加载", ui.CardField{Label: "ICMP / ICMPv6 回显", Value: Label(target), Changed: updated != string(version.Content)})
		}
		return nil
	}
}
