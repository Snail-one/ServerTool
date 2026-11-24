package firewall

import (
	"fmt"
	"strings"
	"time"

	"snail_tool/internal/firewall/loadedrules"
	"snail_tool/internal/firewall/portmapping"
	"snail_tool/internal/firewall/portpolicy"
	"snail_tool/internal/firewall/traffic"
	"snail_tool/internal/ui"
)

func Run(view *ui.UI) error {
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理")
	if !isRoot() {
		return fmt.Errorf("防火墙配置需要 root 权限，请使用 sudo 运行本工具")
	}
	installed, err := ensureNftables(view)
	if err != nil {
		return err
	}
	if !installed {
		return ui.ErrReturnToMenu
	}
	for {
		ui.ClearScreen()
		ui.MenuTitle("防火墙管理")
		autostart := autostartService().Inspect()
		ui.PrintInfoCard("防火墙状态", currentConfigFields(autostart)...)
		showMenu()
		choice, err := view.Ask("请选择（回车刷新状态）：")
		if err != nil {
			return err
		}
		if ui.IsReturnChoice(choice) {
			return ui.ErrReturnToMenu
		}
		switch strings.ToLower(choice) {
		case "":
			continue
		case "1":
			ui.RunAction(view, "规则管理失败，已返回防火墙管理", func() error {
				return runAccessRuleManager(view)
			})
		case "2":
			ui.RunAction(view, "转发规则管理失败，已返回防火墙管理", func() error {
				return runForwardRuleManager(view)
			})
		case "3":
			ui.RunAction(view, "端口映射设置失败，已返回防火墙管理", func() error {
				return (portmapping.Service{
					Store: configStore(), HasManagedConfig: hasManagedConfig,
					ForwardingStatus: ipForwardingService().StatusField,
				}).Run(view)
			})
		case "4":
			ui.RunAction(view, "读取端口策略失败，已返回防火墙管理", (portpolicy.Service{CommandExists: commandExists, CommandOutput: commandOutput}).Show)
		case "5":
			ui.RunAction(view, "读取已加载规则失败，已返回防火墙管理", func() error { return loadedrules.Show(commandOutput) })
		case "c":
			ui.RunAction(view, "读取配置文件失败，已返回防火墙管理", configStore().Show)
		case "v":
			ui.RunAction(view, "编辑或加载配置失败，已返回防火墙管理", editConfig)
		case "r":
			ui.RunAction(view, "加载防火墙配置失败，已返回防火墙管理", func() error {
				return loadConfig(view)
			})
		case "d":
			ui.RunAction(view, "生成防火墙配置失败，已返回防火墙管理", func() error {
				return configure(view)
			})
		case "8":
			ui.RunAction(view, "常用功能设置失败，已返回防火墙管理", func() error {
				return runFeatureSettings(view)
			})
		case "9":
			ui.RunAction(view, "实时查看规则失败，已返回防火墙管理", func() error {
				return traffic.Run(view, time.Second, isRoot, commandExists)
			})
		default:
			ui.InvalidChoice()
			view.Pause()
		}
	}
}

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
