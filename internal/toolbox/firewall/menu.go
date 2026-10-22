package firewall

import (
	"fmt"

	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

func showMenu() {
	fmt.Println()
	ui.MenuOptionHint("1", "查看端口策略", "公网 / 内网 · TCP / UDP")
	ui.MenuOptionHint("2", "查看全部规则", "当前已加载的完整规则")
	ui.MenuOptionHint("3", "放行规则管理", "添加 / 编辑 / 删除 / 排序")
	fmt.Println()
	ui.MenuOptionHint("4", "查看配置文件", configPath)
	ui.MenuOptionHint("5", "编辑配置文件", "保存后校验，可立即加载")
	fmt.Println()
	version, err := readConfigVersion()
	switch {
	case err != nil:
		ui.MenuOptionHint("6", "生成基础配置", "配置状态未知，选择查看原因")
	case hasManagedConfig(string(version.content)):
		ui.MenuOptionHint("6", "重建基础配置", "重新设置端口策略，备份后替换")
	case version.exists:
		ui.MenuOptionHint("6", "生成默认配置", "检测 SSH 端口，备份现有配置后替换")
	default:
		ui.MenuOptionHint("6", "生成默认配置", "检测 SSH 端口，生成后自动加载")
	}
	ui.MenuOptionHint("7", "常用功能设置", "ICMP 回显 / 数据包统计 / 开机自启")
	ui.MenuOptionHint("8", "实时查看规则", "每秒刷新，支持切换规则表")
	fmt.Println()
	ui.MenuShortcutHint("r", "校验加载配置", "使当前配置文件立即生效")
	ui.MenuExit("0/q", "返回主菜单")
	fmt.Println()
}

func runFeatureSettings(view *ui.UI) error {
	for {
		ui.ClearScreen()
		ui.MenuTitle("防火墙管理", "常用功能设置")
		autostart := inspectAutostart()
		installed := commandExists("nft")
		live := liveRules{}
		if installed {
			live = inspectLiveRules()
		}
		fields := echoStateFields(installed, live)
		autostartLabel := autostart.label
		if autostartLabel == "已开启" || autostart.state == "enabled-runtime" {
			autostartLabel = ui.PrimaryText(autostartLabel)
		}
		fields = append(fields,
			packetCounterStateField(installed, live),
			ui.CardField{Label: "开机自启", Value: autostartLabel},
		)
		ui.PrintInfoCard("功能状态", fields...)
		ui.MenuOptionHint("1", "ICMP 回显（Ping）", "允许 / 禁止 IPv4 / IPv6 回显请求")
		ui.MenuOptionHint("2", "数据包统计", "开启 / 关闭规则中的 counter")
		switch {
		case autostart.err != nil:
			ui.MenuOptionHint("3", "开机自启设置", autostart.label+"，选择查看原因")
		case autostart.enabled:
			ui.MenuOptionHint("3", "关闭开机自启", "当前规则保持生效")
		default:
			ui.MenuOptionHint("3", "开启开机自启", "下次开机自动加载配置")
		}
		ui.MenuExit("0/q", "返回防火墙管理")
		choice, err := askRuleChoice(view, "请选择", "", map[string]string{"1": "ping", "2": "counters", "3": "autostart"})
		if err != nil {
			return err
		}
		switch choice {
		case "ping":
			shared.RunAction(view, "设置 ICMP 回显失败，已返回常用功能设置", func() error {
				return configurePing(view)
			})
		case "counters":
			shared.RunAction(view, "设置数据包统计失败，已返回常用功能设置", func() error {
				return configurePacketCounters(view)
			})
		case "autostart":
			shared.RunAction(view, "修改开机自启失败，已返回常用功能设置", func() error {
				return toggleAutostart(view)
			})
		}
	}
}
