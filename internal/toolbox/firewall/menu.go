package firewall

import (
	"fmt"

	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

func showMenu() {
	fmt.Println()
	ui.MenuOptionHint("1", "查看端口策略", "公网 / 内网 · Docker 发布端口")
	ui.MenuOptionHint("2", "查看全部规则", "当前已加载的完整规则")
	ui.MenuOptionHint("3", "访问规则管理", "公网 / 内网端口 · 内网 IP 范围 · 规则配置")
	ui.MenuOptionHint("4", "转发规则管理", "转发端口 · 转发内网范围 · 规则配置")
	ui.MenuOptionHint("5", "端口映射管理", "将本机端口映射到指定 IP 和端口")
	fmt.Println()
	ui.MenuOptionHint("8", "常用功能设置", "自启、访问和转发等开关")
	ui.MenuOptionHint("9", "实时查看规则", "每秒刷新，支持切换规则表")
	fmt.Println()
	ui.MenuShortcutHint("c", "查看配置文件", configPath)
	ui.MenuShortcutHint("v", "编辑配置文件", "保存退出后自动校验并加载")
	version, err := readConfigVersion()
	switch {
	case err != nil:
		ui.MenuShortcutHint("d", "生成基础配置", "配置状态未知，选择查看原因")
	case hasManagedConfig(string(version.content)):
		ui.MenuShortcutHint("d", "重建基础配置", "重新设置端口策略，未转换的旧规则不保留")
	case version.exists:
		ui.MenuShortcutHint("d", "生成默认配置", "备份现有配置后替换，首次配置开启自启")
	default:
		ui.MenuShortcutHint("d", "生成默认配置", "检测 SSH 端口，首次配置开启自启")
	}
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
		autostartLabel := autostart.label
		if autostartLabel == "已开启" || autostart.state == "enabled-runtime" {
			autostartLabel = ui.PrimaryText(autostartLabel)
		}
		fields := []ui.CardField{{Label: "开机自启", Value: autostartLabel}}
		fields = append(fields, echoStateFields(installed, live)...)
		fields = append(fields, packetCounterStateField(installed, live))
		fields = append(fields, forwardingStateField(inspectForwarding()))
		fields = append(fields, sshAccessStateField(), dockerBridgeStateField(), dockerCompatStateField())
		ui.PrintInfoCard("功能状态", fields...)
		switch {
		case autostart.err != nil:
			ui.MenuOptionHint("1", "开机自启设置", autostart.label+"，选择查看原因")
		case autostart.enabled:
			ui.MenuOptionHint("1", "关闭开机自启", "当前规则保持生效")
		default:
			ui.MenuOptionHint("1", "开启开机自启", "下次开机自动加载配置")
		}
		ui.MenuOptionHint("2", "ICMP 回显（Ping）", "允许 / 禁止 IPv4 / IPv6 回显请求")
		ui.MenuOptionHint("3", "数据包统计", "开启 / 关闭规则中的 counter")
		ui.MenuOptionHint("4", "内核转发", "分别开启 / 关闭 IPv4、IPv6 转发")
		ui.MenuOptionHint("5", "SSH 访问", "开放 / 关闭公网、局域网 SSH 端口")
		ui.MenuOptionHint("6", "Docker 流量放行", "网桥流量交给 Docker 自己的规则，需已安装 Docker，默认关闭")
		ui.MenuOptionHint("7", "Docker 放行标记", "给本工具已放行的转发打标记，让 Docker 接受，需已安装 Docker")
		ui.MenuExit("0/q", "返回防火墙管理")
		choice, err := askRuleChoice(view, "请选择", "", map[string]string{"1": "autostart", "2": "ping", "3": "counters", "4": "forwarding", "5": "ssh", "6": "docker", "7": "compat"})
		if err != nil {
			return err
		}
		switch choice {
		case "autostart":
			shared.RunAction(view, "修改开机自启失败，已返回常用功能设置", func() error {
				return toggleAutostart(view)
			})
		case "ping":
			shared.RunAction(view, "设置 ICMP 回显失败，已返回常用功能设置", func() error {
				return configurePing(view)
			})
		case "counters":
			shared.RunAction(view, "设置数据包统计失败，已返回常用功能设置", func() error {
				return configurePacketCounters(view)
			})
		case "forwarding":
			shared.RunAction(view, "设置内核转发失败，已返回常用功能设置", func() error {
				return configureForwarding(view)
			})
		case "ssh":
			shared.RunAction(view, "设置 SSH 访问失败，已返回常用功能设置", func() error {
				return configureSSHAccess(view)
			})
		case "docker":
			shared.RunAction(view, "设置 Docker 流量放行失败，已返回常用功能设置", func() error {
				return configureDockerBridge(view)
			})
		case "compat":
			shared.RunAction(view, "设置 Docker 放行标记失败，已返回常用功能设置", func() error {
				return ConfigureDockerCompat(view)
			})
		}
	}
}
