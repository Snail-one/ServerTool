package firewall

import (
	"fmt"
	"os"

	"snail_tool/internal/ui"
)

func showMenu(autostart autostartStatus) {
	fmt.Println()
	ui.MenuOptionHint("1", "查看端口策略", "公网 / 内网 · TCP / UDP")
	ui.MenuOptionHint("2", "查看全部规则", "当前已加载的完整规则")
	ui.MenuOptionHint("3", "放行规则管理", "添加 / 编辑 / 删除 / 排序")
	fmt.Println()
	ui.MenuOptionHint("4", "查看配置文件", configPath)
	ui.MenuOptionHint("5", "编辑配置文件", "保存后校验，可立即加载")
	fmt.Println()
	if _, err := os.Lstat(configPath); os.IsNotExist(err) {
		ui.MenuOptionHint("6", "生成默认配置", "检测 SSH 端口，生成后自动加载")
	} else {
		ui.MenuOptionHint("6", "重新生成配置", "备份并替换，覆盖手动修改")
	}
	switch {
	case autostart.err != nil:
		ui.MenuOptionHint("7", "开机自启设置", autostart.label+"，选择查看原因")
	case autostart.enabled:
		ui.MenuOptionHint("7", "关闭开机自启", "当前规则保持生效")
	default:
		ui.MenuOptionHint("7", "开启开机自启", "下次开机自动加载配置")
	}
	ui.MenuOptionHint("8", "ICMP 回显（Ping）", "允许 / 禁止 IPv4 / IPv6 回显请求")
	ui.MenuOptionHint("9", "实时查看规则", "每秒刷新，支持切换规则表")
	ui.MenuOptionHint("10", "数据包统计", "开启 / 关闭规则中的 counter")
	fmt.Println()
	ui.MenuShortcutHint("r", "校验加载配置", "使当前配置文件立即生效")
	ui.MenuExit("0/q", "返回主菜单")
	fmt.Println()
}
