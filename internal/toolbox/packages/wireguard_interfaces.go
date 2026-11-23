package packages

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

var wireGuardQuick = func(args ...string) error { return exec.Command("wg-quick", args...).Run() }

type wireGuardInterface struct {
	name   string
	saved  bool
	active bool
}

func wireGuardActiveNames() (map[string]bool, error) {
	if !commandExists("wg") {
		return nil, fmt.Errorf("缺少 wg，无法检测运行状态")
	}
	output, err := commandOutput("wg", "show", "interfaces")
	if err != nil {
		return nil, fmt.Errorf("无法读取 WireGuard 接口: %w", err)
	}
	active := make(map[string]bool)
	for _, name := range strings.Fields(output) {
		if validWireGuardName(name) {
			active[name] = true
		}
	}
	return active, nil
}

func wireGuardInterfaces() ([]wireGuardInterface, error, error) {
	saved, err := os.ReadDir(wireGuardConfigDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, err
	}
	active, activeErr := wireGuardActiveNames()
	profiles := make(map[string]wireGuardInterface)
	for name := range active {
		profiles[name] = wireGuardInterface{name: name, active: true}
	}
	for _, entry := range saved {
		name, ok := strings.CutSuffix(entry.Name(), ".conf")
		if ok && validWireGuardName(name) && entry.Type().IsRegular() {
			profiles[name] = wireGuardInterface{name: name, saved: true, active: active[name]}
		}
	}
	result := make([]wireGuardInterface, 0, len(profiles))
	for _, profile := range profiles {
		result = append(result, profile)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].name < result[j].name })
	return result, activeErr, nil
}

func runWireGuardInterfaces(view *ui.UI) error {
	for {
		profiles, activeErr, err := wireGuardInterfaces()
		if err != nil {
			return err
		}
		ui.ClearScreen()
		ui.MenuTitle("WireGuard", "接口与配置")
		if activeErr != nil {
			ui.PrintField("运行状态", activeErr.Error())
		}
		if len(profiles) == 0 {
			ui.PrintInfoCard("没有可管理的 WireGuard 接口或配置")
			return nil
		}
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
		ui.MenuExit("0/q", "返回 WireGuard")
		choice, err := view.Ask("请选择接口：")
		if err != nil {
			return err
		}
		if shared.IsReturnChoice(choice) {
			return shared.ErrReturnToMenu
		}
		number, err := strconv.Atoi(choice)
		if err != nil || number < 1 || number > len(profiles) {
			ui.InvalidChoice()
			view.Pause()
			continue
		}
		shared.RunAction(view, "接口管理失败，已返回列表", func() error {
			return runWireGuardInterface(view, profiles[number-1].name)
		})
	}
}

type wireGuardUnit struct {
	available bool
	active    bool
	enabled   bool
	state     string
}

func wireGuardUnitName(name string) string { return "wg-quick@" + name + ".service" }

func inspectWireGuardUnit(name string) wireGuardUnit {
	if !commandExists("systemctl") {
		return wireGuardUnit{}
	}
	output, err := commandOutput("systemctl", "show", "--property=LoadState,ActiveState,UnitFileState", wireGuardUnitName(name))
	if err != nil {
		return wireGuardUnit{}
	}
	properties := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			properties[key] = value
		}
	}
	state := properties["UnitFileState"]
	return wireGuardUnit{
		available: properties["LoadState"] == "loaded",
		active:    properties["ActiveState"] == "active",
		enabled:   state == "enabled" || state == "enabled-runtime",
		state:     state,
	}
}

func runWireGuardInterface(view *ui.UI, name string) error {
	for {
		_, saved, err := shared.ReadRegularFile(wireGuardProfilePath(name))
		if err != nil {
			return err
		}
		active, activeErr := wireGuardActiveNames()
		running := active[name]
		unit := inspectWireGuardUnit(name)
		ui.ClearScreen()
		ui.MenuTitle("WireGuard", "接口与配置", name)
		state := "已停止"
		if activeErr != nil {
			state = "检测失败"
		} else if running {
			state = "运行中"
		}
		fields := []ui.CardField{{Label: "接口状态", Value: state}}
		if saved {
			fields = append(fields, ui.CardField{Label: "配置文件", Value: wireGuardProfilePath(name)})
		}
		if unit.available && saved {
			fields = append(fields, ui.CardField{Label: "开机自启", Value: ui.ConfiguredBadge(unit.enabled)})
		}
		ui.PrintInfoCard("WireGuard 接口", fields...)

		actions := make(map[string]func() error)
		add := func(key, label string, action func() error) {
			ui.MenuOption(key, label)
			actions[key] = action
		}
		if running {
			add("1", "查看运行状态", func() error { return showWireGuardRuntime(name) })
		}
		if saved {
			add("2", "查看配置", func() error { return showWireGuardProfile(name) })
			add("3", "编辑配置", func() error { return editWireGuardProfile(view, name) })
			if activeErr == nil && commandExists("wg-quick") {
				if running {
					add("4", "停止接口", func() error { return changeWireGuardRuntime(view, name, "stop") })
					add("5", "重启接口", func() error { return changeWireGuardRuntime(view, name, "restart") })
				} else {
					add("4", "启动接口", func() error { return changeWireGuardRuntime(view, name, "start") })
				}
			}
			if unit.available {
				label := "开启开机自启"
				if unit.enabled {
					label = "关闭开机自启"
				}
				add("6", label, func() error { return changeWireGuardAutostart(view, name, unit) })
			}
			if activeErr == nil && !running && !unit.active && !unit.enabled {
				add("7", "删除配置", func() error { return deleteWireGuardProfile(view, name) })
			}
			add("8", "添加对端", func() error { return addWireGuardPeer(view, name) })
			add("9", "删除对端", func() error { return removeWireGuardPeer(view, name) })
		}
		if !saved && !running {
			return shared.ErrReturnToMenu
		}
		ui.MenuExit("0/q", "返回接口列表")
		choice, err := view.Ask("请选择：")
		if err != nil {
			return err
		}
		if shared.IsReturnChoice(choice) {
			return shared.ErrReturnToMenu
		}
		if choice == "" {
			continue
		}
		if action := actions[choice]; action != nil {
			shared.RunAction(view, "WireGuard 操作失败，已返回接口菜单", action)
		} else {
			ui.InvalidChoice()
			view.Pause()
		}
	}
}

func showWireGuardRuntime(name string) error {
	fields := []ui.CardField{{Label: "接口", Value: name}}
	if commandExists("ip") {
		if output, err := commandOutput("ip", "-brief", "address", "show", "dev", name); err == nil {
			if status := strings.Join(strings.Fields(output), " "); status != "" {
				fields = append(fields, ui.CardField{Label: "接口地址", Value: status})
			}
		}
	}
	peers := make(map[string][]ui.CardField)
	for _, field := range []struct {
		selector, label string
		peer            bool
	}{
		{"public-key", "本机公钥", false}, {"listen-port", "监听端口", false},
		{"endpoints", "端点", true}, {"allowed-ips", "允许地址", true},
		{"latest-handshakes", "最近握手", true}, {"transfer", "接收 / 发送（字节）", true},
	} {
		output, err := commandOutput("wg", "show", name, field.selector)
		if err != nil {
			return fmt.Errorf("读取 WireGuard %s 失败: %w", field.label, err)
		}
		value := strings.TrimSpace(output)
		if !field.peer {
			fields = append(fields, ui.CardField{Label: field.label, Value: value})
			continue
		}
		for _, line := range strings.Split(value, "\n") {
			key, data, ok := strings.Cut(line, "\t")
			if !ok {
				continue
			}
			switch field.selector {
			case "latest-handshakes":
				seconds, err := strconv.ParseInt(strings.TrimSpace(data), 10, 64)
				if err != nil {
					continue
				}
				data = "尚未握手"
				if seconds > 0 {
					data = time.Unix(seconds, 0).Local().Format("2006-01-02 15:04:05 MST")
				}
			case "transfer":
				data = strings.Join(strings.Fields(data), " / ")
			}
			peers[key] = append(peers[key], ui.CardField{Label: field.label, Value: data})
		}
	}
	fields = append(fields, ui.CardField{Label: "对端数量", Value: strconv.Itoa(len(peers))})
	ui.PrintInfoCard("WireGuard 运行状态", fields...)
	keys := make([]string, 0, len(peers))
	for key := range peers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for i, key := range keys {
		peerFields := []ui.CardField{{Label: "公钥", Value: key}}
		ui.PrintSectionCard(fmt.Sprintf("对端 %d", i+1), append(peerFields, peers[key]...)...)
	}
	return nil
}

func changeWireGuardRuntime(view *ui.UI, name, action string) error {
	if !isRoot() {
		return fmt.Errorf("启停 WireGuard 需要 root 权限")
	}
	before, err := readWireGuardProfile(name)
	if err != nil {
		return err
	}
	config, err := parseWireGuardConfig(before)
	if err != nil {
		return err
	}
	unit := inspectWireGuardUnit(name)
	label := map[string]string{"start": "启动", "stop": "停止", "restart": "重启"}[action]
	fields := []ui.CardField{{Label: "接口", Value: name}, {Label: "操作", Value: label}}
	for _, setting := range config.device {
		switch setting.key {
		case "preup", "postup", "predown", "postdown":
			fields = append(fields, ui.CardField{Label: "脚本", Value: "wg-quick 会按配置执行相应的接口脚本"})
		}
		if len(fields) > 2 {
			break
		}
	}
	if action != "start" && strings.EqualFold(wireGuardValue(config.device, "saveconfig"), "true") {
		fields = append(fields, ui.CardField{Label: "SaveConfig", Value: "停止时会将运行配置写回文件，可能覆盖尚未应用的编辑"})
	}
	ui.PrintInfoCard("应用 WireGuard 接口设置", fields...)
	if err := confirmWireGuard(view, "确认"+label+"接口"); err != nil {
		return err
	}
	if err := checkWireGuardProfile(wireGuardProfilePath(name), before); err != nil {
		return err
	}
	active, err := wireGuardActiveNames()
	if err != nil {
		return err
	}
	if active[name] != (action != "start") || inspectWireGuardUnit(name) != unit {
		return fmt.Errorf("接口或服务状态已变化，请返回后重试")
	}

	if action != "start" && strings.EqualFold(wireGuardValue(config.device, "saveconfig"), "true") {
		if err := backupWireGuardProfile(wireGuardProfilePath(name)); err != nil {
			return err
		}
	}
	if unit.active || action == "start" && unit.available {
		if err := commandRun("systemctl", action, wireGuardUnitName(name)); err != nil {
			return fmt.Errorf("systemctl %s WireGuard 失败: %w", action, err)
		}
	} else {
		if action != "start" {
			if err := runWireGuardQuick("down", name); err != nil {
				return err
			}
		}
		if action != "stop" {
			if err := runWireGuardQuick("up", name); err != nil {
				return err
			}
		}
	}
	active, err = wireGuardActiveNames()
	if err != nil {
		return err
	}
	if active[name] != (action != "stop") {
		return fmt.Errorf("操作后接口状态与预期不符")
	}
	ui.PrintSuccessCard("WireGuard 接口已" + label)
	return nil
}

func runWireGuardQuick(action, name string) error {
	// A failed config or custom hook can emit secrets on stderr or stdout.
	if err := wireGuardQuick(action, wireGuardProfilePath(name)); err != nil {
		return fmt.Errorf("wg-quick %s %s 失败，请检查接口配置及系统状态: %w", action, name, err)
	}
	return nil
}

func changeWireGuardAutostart(view *ui.UI, name string, before wireGuardUnit) error {
	if !isRoot() {
		return fmt.Errorf("修改 WireGuard 开机自启需要 root 权限")
	}
	config, err := readWireGuardProfile(name)
	if err != nil {
		return err
	}
	if !before.available {
		return fmt.Errorf("未找到可用的 wg-quick systemd 服务")
	}
	action, label := "enable", "已开启"
	if before.enabled {
		action, label = "disable", "已关闭"
	}
	if err := confirmWireGuard(view, "确认修改 "+name+" 的开机自启设置"); err != nil {
		return err
	}
	if err := checkWireGuardProfile(wireGuardProfilePath(name), config); err != nil {
		return err
	}
	if inspectWireGuardUnit(name) != before {
		return fmt.Errorf("服务状态已变化，请返回后重试")
	}
	args := []string{action, wireGuardUnitName(name)}
	if before.state == "enabled-runtime" {
		args = append([]string{"--runtime"}, args...)
	}
	if err := commandRun("systemctl", args...); err != nil {
		return fmt.Errorf("无法修改 WireGuard 开机自启: %w", err)
	}
	after := inspectWireGuardUnit(name)
	if !after.available || after.enabled != (action == "enable") {
		return fmt.Errorf("无法确认 WireGuard 开机自启已%s", strings.TrimPrefix(label, "已"))
	}
	ui.PrintSuccessCard("WireGuard 开机自启" + label)
	return nil
}

func deleteWireGuardProfile(view *ui.UI, name string) error {
	if !isRoot() {
		return fmt.Errorf("删除 WireGuard 配置需要 root 权限")
	}
	path := wireGuardProfilePath(name)
	before, err := readWireGuardProfile(name)
	if err != nil {
		return err
	}
	ui.PrintWarningCard("删除 WireGuard 配置", ui.CardField{Label: "路径", Value: path})
	if err := confirmWireGuard(view, "确认备份并删除配置"); err != nil {
		return err
	}
	active, err := wireGuardActiveNames()
	if err != nil {
		return err
	}
	unit := inspectWireGuardUnit(name)
	if active[name] || unit.active || unit.enabled {
		return fmt.Errorf("接口仍在运行或已设置开机自启，请先停止接口并关闭自启")
	}
	if err := checkWireGuardProfile(path, before); err != nil {
		return err
	}
	if err := backupWireGuardProfile(path); err != nil {
		return err
	}
	current, err := readWireGuardProfile(name)
	if err != nil {
		return err
	}
	if !bytes.Equal(before, current) {
		return fmt.Errorf("配置文件已变化，已取消删除")
	}
	if err := shared.RemoveRegularFile(path); err != nil {
		return err
	}
	ui.PrintSuccessCard("WireGuard 配置已备份并删除")
	return nil
}
