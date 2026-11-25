package status

import (
	"fmt"
	"os"
	"strings"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/firewall/configuration/ownership"
	"snail_tool/internal/firewall/loadedrules"
	"snail_tool/internal/firewall/settings/autostart"
	"snail_tool/internal/firewall/settings/counters"
	"snail_tool/internal/firewall/settings/ipforwarding"
	"snail_tool/internal/firewall/settings/ping"
	"snail_tool/internal/ui"
)

type Service struct {
	Store         configuration.Store
	CommandExists func(string) bool
	Forwarding    ipforwarding.Service
}

func (service Service) Fields(boot autostart.Status) []ui.CardField {
	installed := service.CommandExists("nft")
	live := loadedrules.Snapshot{}
	if installed {
		live = loadedrules.Inspect(service.Store.CommandOutput)
	}
	software := ui.InstallationBadge(installed)
	if live.Version != "" {
		software += " v" + strings.TrimPrefix(live.Version, "v")
	}
	data, fileErr := os.ReadFile(service.Store.Path)
	autostartLabel := boot.Label
	if autostartLabel == "已开启" || boot.State == "enabled-runtime" {
		autostartLabel = ui.PrimaryText(autostartLabel)
	}
	configOwnership := ownership.Field(data, fileErr)
	if configOwnership.Value == "已接管" || configOwnership.Value == "部分接管" {
		configOwnership.Value = ui.PrimaryText(configOwnership.Value)
	}
	fields := []ui.CardField{
		firewallStateField(installed, live),
		configOwnership,
		{Label: "开机自启", Value: autostartLabel},
		counters.StateField(installed, live.InputActive, live.JSON, live.Err),
		service.Forwarding.StatusField(),
	}
	fields = append(fields, ping.StateFields(installed, live.InputActive, live.JSON, live.Err)...)
	if installed && live.Err == nil {
		if live.InputActive {
			policy := strings.TrimPrefix(live.InputPolicy, "input：")
			policy = strings.ReplaceAll(policy, "默认丢弃（drop）", ui.PrimaryText("默认丢弃（drop）"))
			fields = append(fields, ui.CardField{Label: "入站策略", Value: policy})
		}
		if live.SnailLoaded {
			fields = append(fields, ui.CardField{Label: "本工具规则", Value: fmt.Sprintf("%d 条链 · %d 条规则", live.SnailChains, live.SnailRules)})
		}
		if !live.InputActive && live.OtherInputChains > 0 {
			fields = append(fields, ui.CardField{Label: "其他入站链", Value: fmt.Sprintf("%d 条，选择 5 查看全部规则", live.OtherInputChains)})
		}
	}
	sync := service.Store.SyncField(data, fileErr, installed, live.JSON, live.Err, service.CommandExists)
	state, _, _ := strings.Cut(sync.Value, " · ")
	if state == "已同步" || state == "未同步" {
		sync.Value, sync.Detail = state, ""
	}
	if state == "已同步" {
		sync.Value = ui.PrimaryText(sync.Value)
	}
	return append(fields, sync, ui.CardField{Label: "nftables", Value: software})
}

func firewallStateField(installed bool, live loadedrules.Snapshot) ui.CardField {
	field := ui.CardField{Label: "防火墙"}
	switch {
	case !installed:
		field.Value = "无法检测（未安装 nftables）"
	case live.Err != nil:
		field.Value, field.Detail = "检测失败", live.Err.Error()
	case live.InputActive:
		field.Value = ui.PrimaryText("已开启（本工具入站规则）")
	case live.SnailDormant:
		field.Value = "未开启（本工具表已停用）"
	case live.SnailLoaded:
		field.Value = "未开启（入站链未挂接）"
	default:
		field.Value = "未开启（本工具）"
	}
	return field
}
