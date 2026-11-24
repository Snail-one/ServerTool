package wireguard

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"snail_tool/internal/input"
	"snail_tool/internal/system"
	"snail_tool/internal/system/packagemanager"
	"snail_tool/internal/ui"
)

var (
	commandExists = system.CommandExists
	commandRun    = system.Run
	commandOutput = packagemanager.QueryOutput
	isRoot        = system.IsRoot
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

func Badge() string { return detectWireGuardTools().badge() }

func Run(view *ui.UI) error {
	invalidChoice := false
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
		}
		profiles, activeErr, readErr := wireGuardInterfaces()
		if tools.wg && activeErr != nil {
			fields = append(fields, ui.CardField{Label: "运行状态", Value: "读取失败", Detail: activeErr.Error()})
		}
		if readErr != nil {
			fields = append(fields, ui.CardField{Label: "配置目录", Value: "读取失败", Detail: readErr.Error()})
		}
		if len(profiles) == 0 && readErr == nil && (!tools.wg || activeErr == nil) {
			fields = append(fields, ui.CardField{Label: "接口", Value: "暂无接口或配置"})
		}
		fields = append(fields, wireGuardInterfaceSummaries(profiles)...)
		ui.PrintInfoCard("WireGuard", fields...)
		for i, profile := range profiles {
			state := "已停止"
			switch {
			case activeErr != nil:
				state = "状态未知"
			case profile.active:
				state = "运行中"
			}
			detail := "已有配置"
			if !profile.saved {
				detail = "仅运行时接口"
			}
			ui.MenuOptionStatusHint(strconv.Itoa(i+1), profile.name, ui.SoftwareBadge(state, profile.active), detail)
		}
		if len(profiles) > 0 {
			fmt.Println()
		}
		if tools.wg {
			ui.MenuShortcutHint("n", "新建配置", "支持手动填写隧道地址")
		}
		if !tools.ready() {
			ui.MenuShortcutHint("i", "安装 WireGuard 工具", "使用系统软件源安装 wg 和 wg-quick")
		}
		if tools.wg || tools.quick {
			ui.MenuShortcutHint("u", "卸载 WireGuard 工具", "停止接口并关闭自启，保留配置")
		}
		ui.MenuOption("r", "刷新状态")
		ui.MenuExit("0/q", "返回主菜单")

		if invalidChoice {
			ui.InvalidChoice()
			invalidChoice = false
		}
		choice, err := view.Ask("请选择：")
		if err != nil {
			return err
		}
		if ui.IsReturnChoice(choice) {
			return ui.ErrReturnToMenu
		}
		choice = strings.ToLower(choice)
		number, numberErr := input.Number(choice, 1, len(profiles))
		switch {
		case choice == "" || choice == "r":
			continue
		case choice == "n" && tools.wg:
			ui.RunAction(view, "新建 WireGuard 配置失败，已返回管理菜单", func() error {
				return createWireGuardProfile(view)
			})
		case choice == "i" && !tools.ready():
			ui.RunAction(view, "安装 WireGuard 失败，已返回管理菜单", func() error {
				return installWireGuard(view)
			})
		case choice == "u" && (tools.wg || tools.quick):
			ui.RunAction(view, "卸载 WireGuard 失败，已返回管理菜单", func() error {
				return uninstallWireGuard(view)
			})
		case numberErr == nil:
			ui.RunAction(view, "接口管理失败，已返回 WireGuard", func() error {
				return runWireGuardInterface(view, profiles[number-1].name)
			})
		default:
			invalidChoice = true
		}
	}
}

func installWireGuard(view *ui.UI) error {
	if !isRoot() {
		return fmt.Errorf("安装 WireGuard 需要 root 权限，请使用 sudo 运行本工具")
	}
	manager, err := packagemanager.Detect(commandExists)
	if err != nil {
		return err
	}
	confirmed, err := view.Confirm(fmt.Sprintf("使用 %s 安装 %s？(y/N，0/q 返回)：", manager.Name, wireGuardPackage))
	if err != nil {
		return err
	}
	if !confirmed {
		return ui.ErrReturnToMenu
	}
	if err := manager.Install(commandRun, []string{wireGuardPackage}); err != nil {
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
	manager, err := packagemanager.Detect(commandExists)
	if err != nil {
		return err
	}
	installed, err := packagemanager.Installed(manager, commandOutput)
	if err != nil {
		return err
	}
	candidates := []string{wireGuardPackage}
	if manager.Name == "apk" {
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
	targets, err := wireGuardUninstallTargets()
	if err != nil {
		return err
	}
	fields := []ui.CardField{
		{Label: "软件包", Value: strings.Join(names, "、")},
		{Label: "配置目录", Value: "保留 /etc/wireguard"},
	}
	var stopping, disabling []string
	for _, target := range targets {
		if target.active || target.unit.active {
			stopping = append(stopping, target.name)
		}
		if target.unit.enabled {
			disabling = append(disabling, target.name)
		}
	}
	if len(stopping) > 0 {
		fields = append(fields, ui.CardField{Label: "停止接口", Value: strings.Join(stopping, "、"), Detail: "通过这些接口建立的连接会断开"})
	}
	if len(disabling) > 0 {
		fields = append(fields, ui.CardField{Label: "关闭开机自启", Value: strings.Join(disabling, "、")})
	}
	ui.PrintWarningCard("卸载 WireGuard 工具", fields...)
	confirmed, err := view.Confirm("确认卸载？(y/N，0/q 返回)：")
	if err != nil {
		return err
	}
	if !confirmed {
		return ui.ErrReturnToMenu
	}
	current, err := wireGuardUninstallTargets()
	if err != nil {
		return err
	}
	if !slices.Equal(current, targets) {
		return fmt.Errorf("WireGuard 接口或自启状态已变化，请重新确认卸载")
	}
	for _, target := range targets {
		if err := stopWireGuardForUninstall(target); err != nil {
			return fmt.Errorf("%s 停止失败，软件包未卸载: %w", target.name, err)
		}
		if target.unit.enabled {
			if err := setWireGuardAutostart(target.name, target.unit, "disable"); err != nil {
				return fmt.Errorf("%s 关闭开机自启失败，软件包未卸载: %w", target.name, err)
			}
		}
	}

	args := []string{"remove", "-y"}
	switch manager.Name {
	case "dnf", "yum":
		args = append(args, "--setopt=clean_requirements_on_remove=False")
	case "pacman":
		args = []string{"-R", "--noconfirm"}
	case "zypper":
		args = []string{"--non-interactive", "remove"}
	case "apk":
		args = []string{"del"}
	}
	if err := commandRun(manager.Name, append(args, names...)...); err != nil {
		return fmt.Errorf("%s 卸载 WireGuard 软件包失败: %w", manager.Name, err)
	}
	installed, err = packagemanager.Installed(manager, commandOutput)
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

type wireGuardUninstallTarget struct {
	wireGuardInterface
	unit wireGuardUnit
}

func wireGuardUninstallTargets() ([]wireGuardUninstallTarget, error) {
	var active map[string]bool
	if commandExists("wg") {
		var err error
		active, err = wireGuardActiveNames()
		if err != nil {
			return nil, err
		}
	} else {
		output, err := commandOutput("ip", "-o", "link", "show", "type", "wireguard")
		if err != nil {
			return nil, fmt.Errorf("无法确认 WireGuard 接口是否已停止: %w", err)
		}
		if strings.TrimSpace(output) != "" {
			return nil, fmt.Errorf("有 WireGuard 接口存在且缺少 wg，请恢复工具或先停止接口")
		}
	}
	profiles, err := wireGuardProfiles(active)
	if err != nil {
		return nil, err
	}
	targets := make([]wireGuardUninstallTarget, 0, len(profiles))
	for _, profile := range profiles {
		unit := inspectWireGuardUnit(profile.name)
		if profile.active && !unit.active && (!profile.saved || !commandExists("wg-quick")) {
			return nil, fmt.Errorf("无法自动停止 %s，请先使用启动该接口的方式停止再卸载", profile.name)
		}
		targets = append(targets, wireGuardUninstallTarget{wireGuardInterface: profile, unit: unit})
	}
	return targets, nil
}

func stopWireGuardForUninstall(target wireGuardUninstallTarget) error {
	if !target.active && !target.unit.active {
		return nil
	}
	if target.saved {
		data, err := readWireGuardProfile(target.name)
		if err != nil {
			return err
		}
		config, err := parseWireGuardConfig(data)
		if err != nil {
			return err
		}
		if strings.EqualFold(wireGuardValue(config.device, "saveconfig"), "true") {
			if err := backupWireGuardProfile(wireGuardProfilePath(target.name)); err != nil {
				return err
			}
		}
	}
	return applyWireGuardRuntime(target.name, "stop", target.unit.active)
}
