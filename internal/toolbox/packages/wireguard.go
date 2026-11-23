package packages

import (
	"fmt"
	"strings"

	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

const wireGuardPackage = "wireguard-tools"

type wireGuardTools struct {
	wg, quick bool
}

func detectWireGuardTools() wireGuardTools {
	return wireGuardTools{wg: commandExists("wg"), quick: commandExists("wg-quick")}
}

func (tools wireGuardTools) ready() bool { return tools.wg && tools.quick }

func (tools wireGuardTools) badge() string {
	switch {
	case tools.ready():
		return ui.SoftwareBadge("已安装", true)
	case tools.wg || tools.quick:
		return ui.SoftwareBadge("部分安装", false)
	default:
		return ui.SoftwareBadge("未安装", false)
	}
}

func WireGuardBadge() string { return detectWireGuardTools().badge() }

func RunWireGuard(view *ui.UI) error {
	for {
		ui.ClearScreen()
		ui.MenuTitle("WireGuard")
		tools := detectWireGuardTools()
		fields := []ui.CardField{{Label: "工具状态", Value: tools.badge()}}
		if tools.wg {
			version, err := commandOutput("wg", "--version")
			if err != nil || strings.TrimSpace(version) == "" {
				version = "读取失败"
			}
			fields = append(fields, ui.CardField{Label: "版本", Value: strings.TrimSpace(version)})
			interfaces, err := commandOutput("wg", "show", "interfaces")
			value := "无"
			if names := strings.Fields(interfaces); len(names) > 0 {
				value = strings.Join(names, "、")
			}
			if err != nil {
				value = "读取失败"
			}
			fields = append(fields, ui.CardField{Label: "当前接口", Value: value})
		}
		ui.PrintInfoCard("WireGuard", fields...)
		if !tools.ready() {
			ui.MenuOptionHint("1", "安装 WireGuard 工具", "使用系统软件源安装 wg 和 wg-quick")
		}
		if tools.wg || tools.quick {
			ui.MenuOptionHint("2", "卸载 WireGuard 工具", "保留 /etc/wireguard 配置")
		}
		ui.MenuOption("3", "接口与配置")
		if tools.wg {
			ui.MenuOption("4", "新建配置")
		}
		ui.MenuOption("5", "导入配置")
		ui.MenuOption("r", "刷新状态")
		ui.MenuExit("0/q", "返回主菜单")

		choice, err := view.Ask("请选择：")
		if err != nil {
			return err
		}
		if shared.IsReturnChoice(choice) {
			return shared.ErrReturnToMenu
		}
		switch {
		case choice == "" || strings.EqualFold(choice, "r"):
			continue
		case choice == "1" && !tools.ready():
			shared.RunAction(view, "安装 WireGuard 失败，已返回管理菜单", func() error {
				return installWireGuard(view)
			})
		case choice == "2" && (tools.wg || tools.quick):
			shared.RunAction(view, "卸载 WireGuard 失败，已返回管理菜单", func() error {
				return uninstallWireGuard(view)
			})
		case choice == "3":
			shared.RunAction(view, "读取 WireGuard 接口失败，已返回管理菜单", func() error {
				return runWireGuardInterfaces(view)
			})
		case choice == "4" && tools.wg:
			shared.RunAction(view, "新建 WireGuard 配置失败，已返回管理菜单", func() error {
				return createWireGuardProfile(view, false)
			})
		case choice == "5":
			shared.RunAction(view, "导入 WireGuard 配置失败，已返回管理菜单", func() error {
				return createWireGuardProfile(view, true)
			})
		default:
			ui.InvalidChoice()
			view.Pause()
		}
	}
}

func installWireGuard(view *ui.UI) error {
	if !isRoot() {
		return fmt.Errorf("安装 WireGuard 需要 root 权限，请使用 sudo 运行本工具")
	}
	manager, err := detectPackageManager()
	if err != nil {
		return err
	}
	confirmed, err := view.Confirm(fmt.Sprintf("使用 %s 安装 %s？(y/N，0/q 返回)：", manager.name, wireGuardPackage))
	if err != nil {
		return err
	}
	if !confirmed {
		return shared.ErrReturnToMenu
	}
	if err := installPackages(manager, []commandLineTool{{packageName: wireGuardPackage}}); err != nil {
		return err
	}
	if !detectWireGuardTools().ready() {
		return fmt.Errorf("安装完成后仍缺少 wg 或 wg-quick，请检查软件包和 PATH")
	}
	ui.PrintSuccessCard("WireGuard 工具已安装")
	return nil
}

func uninstallWireGuard(view *ui.UI) error {
	if !isRoot() {
		return fmt.Errorf("卸载 WireGuard 需要 root 权限，请使用 sudo 运行本工具")
	}
	manager, err := detectPackageManager()
	if err != nil {
		return err
	}
	installed, err := queryInstalledPackages(manager)
	if err != nil {
		return err
	}
	candidates := []string{wireGuardPackage}
	if manager.name == "apk" {
		candidates = append(candidates, "wireguard-tools-wg-quick", "wireguard-tools-wg")
	}
	var names []string
	for _, name := range candidates {
		if installed[name] {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return fmt.Errorf("系统包管理器未登记 WireGuard 工具，请按原安装方式卸载")
	}
	fields := []ui.CardField{
		{Label: "软件包", Value: strings.Join(names, "、")},
		{Label: "配置目录", Value: "保留 /etc/wireguard"},
	}
	if commandExists("wg") {
		interfaces, err := commandOutput("wg", "show", "interfaces")
		if active := strings.Fields(interfaces); err == nil && len(active) > 0 {
			fields = append(fields, ui.CardField{Label: "当前接口", Value: strings.Join(active, "、"), Detail: "卸载软件包可能影响这些接口"})
		}
	}
	ui.PrintWarningCard("卸载 WireGuard 工具", fields...)
	confirmed, err := view.Confirm("确认卸载？(y/N，0/q 返回)：")
	if err != nil {
		return err
	}
	if !confirmed {
		return shared.ErrReturnToMenu
	}

	args := []string{"remove", "-y"}
	switch manager.name {
	case "dnf", "yum":
		args = append(args, "--setopt=clean_requirements_on_remove=False")
	case "pacman":
		args = []string{"-R", "--noconfirm"}
	case "zypper":
		args = []string{"--non-interactive", "remove"}
	case "apk":
		args = []string{"del"}
	}
	if err := commandRun(manager.name, append(args, names...)...); err != nil {
		return fmt.Errorf("%s 卸载 WireGuard 软件包失败: %w", manager.name, err)
	}
	installed, err = queryInstalledPackages(manager)
	if err != nil {
		return fmt.Errorf("卸载后无法确认软件包状态: %w", err)
	}
	for _, name := range names {
		if installed[name] {
			return fmt.Errorf("软件包 %s 仍然安装，请检查依赖关系", name)
		}
	}
	ui.PrintSuccessCard("WireGuard 软件包已卸载")
	return nil
}
