package firewall

import (
	"fmt"

	rulespkg "snail_tool/internal/firewall/internal/rules"
	"snail_tool/internal/firewall/loadedrules"
	"snail_tool/internal/firewall/settings"
	"snail_tool/internal/firewall/settings/counters"
	"snail_tool/internal/firewall/settings/ping"
	"snail_tool/internal/ui"
)

func showMenu() {
	fmt.Println()
	ui.MenuOptionHint("1", "访问规则管理", "公网 / 内网端口 · 内网 IP 范围 · 规则配置")
	ui.MenuOptionHint("2", "转发规则管理", "转发端口 · 内网范围 · 规则配置 · Docker 放行")
	ui.MenuOptionHint("3", "端口映射管理", "将本机端口映射到指定 IP 和端口")
	fmt.Println()
	ui.MenuOptionHint("4", "查看端口策略", "公网 / 内网 · Docker 发布端口")
	ui.MenuOptionHint("5", "查看全部规则", "当前已加载的完整规则")
	fmt.Println()
	ui.MenuOptionHint("8", "常用功能设置", "自启、访问和转发等开关")
	ui.MenuOptionHint("9", "实时查看规则", "每秒刷新，支持切换规则表")
	fmt.Println()
	ui.MenuShortcutHint("c", "查看配置文件", configPath)
	ui.MenuShortcutHint("v", "编辑配置文件", "保存退出后自动校验并加载")
	version, err := configStore().Read()
	switch {
	case err != nil:
		ui.MenuShortcutHint("d", "生成基础配置", "配置状态未知，选择查看原因")
	case hasManagedConfig(string(version.Content)):
		ui.MenuShortcutHint("d", "重建基础配置", "重新设置端口策略，未转换的旧规则不保留")
	case version.Exists:
		ui.MenuShortcutHint("d", "生成默认配置", "备份现有配置后替换，首次配置开启自启")
	default:
		ui.MenuShortcutHint("d", "生成默认配置", "检测 SSH 端口，首次配置开启自启")
	}
	ui.MenuShortcutHint("r", "校验加载配置", "使当前配置文件立即生效")
	ui.MenuExit("0/q", "返回主菜单")
}

func confirmFeatureChange(view *ui.UI, question string) (bool, error) {
	return ui.ConfirmMenuChange(view, question, "常用功能设置")
}

func runFeatureSettings(view *ui.UI) error {
	return settings.Run(view, settings.Actions{
		Inspect:    inspectFeatureSettings,
		Autostart:  autostartService().Toggle,
		Ping:       configurePing,
		Counters:   configurePacketCounters,
		Forwarding: forwardingService().Run,
		SSH:        sshAccessService().Run,
	})
}

func inspectFeatureSettings() settings.Snapshot {
	autostart := autostartService().Inspect()
	installed := commandExists("nft")
	live := loadedrules.Snapshot{}
	if installed {
		live = inspectLiveRules()
	}
	autostartLabel := autostart.Label
	if autostartLabel == "已开启" || autostart.State == "enabled-runtime" {
		autostartLabel = ui.PrimaryText(autostartLabel)
	}
	fields := []ui.CardField{{Label: "开机自启", Value: autostartLabel}}
	fields = append(fields, echoStateFields(installed, live)...)
	fields = append(fields, packetCounterStateField(installed, live))
	fields = append(fields, forwardingService().StatusField())
	fields = append(fields, sshAccessService().StateField())
	return settings.Snapshot{
		Fields: fields, AutostartUnavailable: autostart.Err != nil, AutostartEnabled: autostart.Enabled,
	}
}

func packetCounterStateField(installed bool, live loadedrules.Snapshot) ui.CardField {
	return counters.StateField(installed, live.InputActive, live.JSON, live.Err)
}

func echoStateFields(installed bool, live loadedrules.Snapshot) []ui.CardField {
	return ping.StateFields(installed, live.InputActive, live.JSON, live.Err)
}

func validateCounterConfig(content string) error {
	if _, err := rulespkg.ManagedRules(content); err != nil {
		return err
	}
	_, _, err := ping.Read(content)
	return err
}

func withPacketCounters(content string, enabled bool) (string, error) {
	return counters.Replace(content, enabled, validateCounterConfig)
}

func configurePacketCounters(view *ui.UI) error {
	return (counters.Service{Store: configStore(), IsRoot: isRoot, Validate: validateCounterConfig}).Run(view)
}

func configurePing(view *ui.UI) error {
	return (ping.Service{
		Store: configStore(), IsRoot: isRoot, HasManagedConfig: hasManagedConfig, CommandExists: commandExists,
		InspectLive: func() (string, bool, error) {
			live := inspectLiveRules()
			return live.JSON, live.InputActive, live.Err
		},
	}).Run(view)
}
