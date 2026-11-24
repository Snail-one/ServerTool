package firewall

import (
	"fmt"
	"os"
	"strings"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/firewall/forward/dockerbridge"
	rulespkg "snail_tool/internal/firewall/internal/rules"
	"snail_tool/internal/firewall/loadedrules"
	autostartconfig "snail_tool/internal/firewall/settings/autostart"
	"snail_tool/internal/firewall/settings/ping"
	"snail_tool/internal/firewall/settings/sshaccess"
	"snail_tool/internal/ui"
)

func currentConfigFields(autostart autostartconfig.Status) []ui.CardField {
	installed := commandExists("nft")
	live := loadedrules.Snapshot{}
	if installed {
		live = inspectLiveRules()
	}
	software := ui.InstallationBadge(installed)
	if live.Version != "" {
		software += " v" + strings.TrimPrefix(live.Version, "v")
	}
	data, fileErr := os.ReadFile(configPath)
	autostartLabel := autostart.Label
	if autostartLabel == "已开启" || autostart.State == "enabled-runtime" {
		autostartLabel = ui.PrimaryText(autostartLabel)
	}
	ownership := toolOwnershipField(data, fileErr)
	if ownership.Value == "已接管" || ownership.Value == "部分接管" {
		ownership.Value = ui.PrimaryText(ownership.Value)
	}
	fields := []ui.CardField{
		firewallStateField(installed, live),
		ownership,
		{Label: "开机自启", Value: autostartLabel},
		packetCounterStateField(installed, live),
		ipForwardingService().StatusField(),
	}
	fields = append(fields, echoStateFields(installed, live)...)
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
	sync := configSyncField(data, fileErr, installed, live)
	state, _, _ := strings.Cut(sync.Value, " · ")
	if state == "已同步" || state == "未同步" {
		sync.Value, sync.Detail = state, ""
	}
	if state == "已同步" {
		sync.Value = ui.PrimaryText(sync.Value)
	}
	return append(fields, sync, ui.CardField{Label: "nftables", Value: software})
}

// Ownership is configuration metadata, independent of kernel activation and
// synchronization. A matching table name alone does not establish ownership.
func toolOwnershipField(data []byte, fileErr error) ui.CardField {
	field := ui.CardField{Label: "工具接管", Value: "未接管"}
	if os.IsNotExist(fileErr) {
		field.Value = "未接管（配置不存在）"
		return field
	}
	if fileErr != nil {
		field.Value, field.Detail = "无法判断", fileErr.Error()
		return field
	}
	content := string(data)
	mainStart, mainEnd := -1, -1
	for _, markers := range [][2]string{{configuration.ManagedBegin, configuration.ManagedEnd}, {rulespkg.AccessBegin, rulespkg.AccessEnd}, {sshaccess.LegacyBegin, sshaccess.LegacyEnd}, {ping.Begin, ping.End}, {dockerbridge.Begin, dockerbridge.End}} {
		begins, ends := strings.Count(content, markers[0]), strings.Count(content, markers[1])
		if begins == 0 && ends == 0 {
			continue
		}
		begin, end := strings.Index(content, markers[0]), strings.Index(content, markers[1])
		if begins != 1 || ends != 1 || end < begin {
			field.Value = "标记异常"
			return field
		}
		field.Value = "部分接管"
		if markers[0] == configuration.ManagedBegin {
			mainStart, mainEnd = begin, end+len(configuration.ManagedEnd)
		}
	}
	if mainStart >= 0 {
		outside, err := configuration.Tokens(content[:mainStart] + content[mainEnd:])
		if err == nil && len(outside) == 0 {
			field.Value = "已接管"
		}
	}
	return field
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

func inspectLiveRules() loadedrules.Snapshot {
	return loadedrules.Inspect(commandOutput)
}
