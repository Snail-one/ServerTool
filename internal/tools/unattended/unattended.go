package unattended

import (
	"fmt"
	"strconv"
	"strings"

	"snail_tool/internal/system"
	"snail_tool/internal/system/packagemanager"
	"snail_tool/internal/ui"
)

const (
	configPath    = "/etc/apt/apt.conf.d/99-z-servertool-unattended-upgrades.conf"
	refreshTimer  = "apt-daily.timer"
	upgradeTimer  = "apt-daily-upgrade.timer"
	managedHeader = "// Managed by ServerTool.\n"
)

type manager struct {
	path     string
	exists   func(string) bool
	root     func() bool
	query    func(string, ...string) (string, error)
	run      func(string, ...string) error
	timerDir string
}

func Run(view *ui.UI) error {
	service := manager{path: configPath, exists: system.CommandExists, root: system.IsRoot, query: packagemanager.QueryOutput, run: system.Run, timerDir: "/etc/systemd/system"}
	if !service.exists("apt-get") || !service.exists("apt-config") {
		return fmt.Errorf("此功能需要 APT 和 unattended-upgrades")
	}
	return service.menu(view)
}

type settings struct {
	periodic, refresh, upgrade, reboot, preferred string
	shutdown, preferredShutdown                   string
}

func (service manager) settings() (settings, error) {
	output, err := service.query("apt-config", "shell",
		"PERIODIC", "APT::Periodic::Enable", "REFRESH", "APT::Periodic::Update-Package-Lists",
		"UPGRADE", upgradeKey, "REBOOT", rebootKey+"/b", "PREFERRED", intervalKey,
		"SHUTDOWN", shutdownKey+"/b", "PREFERRED_SHUTDOWN", shutdownPreference)
	if err != nil {
		return settings{}, fmt.Errorf("读取 APT 生效设置失败: %w", err)
	}
	values := assignments(output)
	return settings{periodic: values["PERIODIC"], refresh: values["REFRESH"], upgrade: values["UPGRADE"],
		reboot: values["REBOOT"], preferred: values["PREFERRED"], shutdown: values["SHUTDOWN"],
		preferredShutdown: values["PREFERRED_SHUTDOWN"]}, nil
}

func assignments(output string) map[string]string {
	values := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			if strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") && len(value) >= 2 {
				value = strings.ReplaceAll(value[1:len(value)-1], "'\\''", "'")
			}
			values[key] = value
		}
	}
	return values
}

func intervalLabel(value string) string {
	if value == "always" {
		return "每次定时任务触发时"
	}
	if value == "" || value == "0" {
		return "关闭"
	}
	if days, err := strconv.Atoi(value); err == nil && days > 0 {
		return fmt.Sprintf("每 %d 天", days)
	}
	return "未知间隔"
}

type timer struct {
	load, file, active, next string
}

func (service manager) timer(name string) (timer, error) {
	if !service.exists("systemctl") {
		return timer{}, fmt.Errorf("未找到 systemctl")
	}
	output, err := service.query("systemctl", "show", "--property=LoadState,UnitFileState,ActiveState,NextElapseUSecRealtime", name)
	if err != nil {
		return timer{}, fmt.Errorf("查询 %s 失败: %w", name, err)
	}
	values := assignments(output)
	return timer{values["LoadState"], values["UnitFileState"], values["ActiveState"], values["NextElapseUSecRealtime"]}, nil
}

func (state timer) running() bool {
	return state.file == "enabled" && state.active == "active"
}

func (service manager) fields() []ui.CardField {
	fields := []ui.CardField{{Label: "软件包", Value: ui.InstallationBadge(service.exists("unattended-upgrade"))}}
	current, err := service.settings()
	if err != nil {
		fields = append(fields, ui.CardField{Label: "APT 设置", Value: "检测失败", Detail: err.Error()})
	} else {
		reboot := "关闭"
		if current.reboot == "true" || current.reboot == "1" {
			reboot = "开启"
		}
		shutdown := "关闭"
		if current.shutdown == "true" {
			shutdown = "开启"
		}
		if current.periodic == "0" {
			fields = append(fields, ui.CardField{Label: "APT 定时任务", Value: "全局已关闭"})
		}
		fields = append(fields,
			ui.CardField{Label: "刷新软件包索引", Value: intervalLabel(current.refresh)},
			ui.CardField{Label: "自动安装更新", Value: intervalLabel(current.upgrade)},
			ui.CardField{Label: "自动重启", Value: reboot},
			ui.CardField{Label: "关机时安装更新", Value: shutdown},
		)
	}
	for _, name := range []string{refreshTimer, upgradeTimer} {
		state, err := service.timer(name)
		field := ui.CardField{Label: name, Value: state.file + " / " + state.active}
		if err != nil {
			field.Value, field.Detail = "检测失败", err.Error()
		} else if state.load != "loaded" {
			field.Value = state.load
		} else if state.next != "" && state.next != "n/a" {
			field.Detail = "下次触发：" + state.next
		}
		fields = append(fields, field)
	}
	return fields
}

func (service manager) change(view *ui.UI, enable bool) error {
	if !service.root() {
		return fmt.Errorf("修改自动更新需要 root 权限")
	}
	previous, exists, err := readManaged(service.path, managedHeader)
	if err != nil {
		return err
	}
	message := "关闭定时和关机自动安装更新，保留 APT 定时器与更新策略"
	if enable {
		message = "按当前频率及软件源策略开启自动更新"
		if !service.exists("unattended-upgrade") {
			message = "安装 unattended-upgrades，然后" + message
		}
	}
	ui.PrintField("配置文件", service.path)
	confirmed, err := view.Confirm("确认" + message + "？(y/N)：")
	if err != nil {
		return err
	}
	if !confirmed {
		return ui.ErrReturnToMenu
	}
	return service.apply(enable, previous, exists)
}

func (service manager) apply(enable bool, previous []byte, exists bool) error {
	var timers []string
	if err := checkSnapshot(service.path, previous, exists); err != nil {
		return err
	}
	current, err := service.settings()
	if err != nil {
		return err
	}
	options := map[string]string{upgradeKey: "0"}
	if !enable {
		options[shutdownKey] = "false"
		if current.shutdown == "true" {
			options[shutdownPreference] = "true"
		}
	}
	if activeInterval(current.upgrade) {
		options[intervalKey] = current.upgrade
	}
	if enable {
		if current.periodic == "0" {
			return fmt.Errorf("APT::Periodic::Enable 已全局关闭，请先检查 APT 配置")
		}
		for _, name := range []string{refreshTimer, upgradeTimer} {
			state, err := service.timer(name)
			if err != nil {
				return err
			}
			if state.load != "loaded" || state.file == "masked" || state.file == "masked-runtime" {
				return fmt.Errorf("定时器 %s 不可用（%s）", name, state.load)
			}
			if !state.running() {
				timers = append(timers, name)
			}
		}
		if !service.exists("unattended-upgrade") {
			apt, err := packagemanager.Detect(service.exists)
			if err != nil {
				return err
			}
			if err := apt.Install(service.run, []string{"unattended-upgrades"}); err != nil {
				return err
			}
			if !service.exists("unattended-upgrade") {
				return fmt.Errorf("安装后仍未找到 unattended-upgrade")
			}
		}
		options[upgradeKey] = current.upgrade
		if !activeInterval(options[upgradeKey]) {
			options[upgradeKey] = current.preferred
			if !activeInterval(options[upgradeKey]) {
				options[upgradeKey] = "1"
			}
		}
		if current.refresh == "" {
			options[refreshKey] = "1"
		}
		if current.preferredShutdown == "true" {
			options[shutdownKey] = "true"
		}
	}
	if err := service.saveOptions(previous, exists, options); err != nil {
		return err
	}
	if len(timers) == 0 {
		return nil
	}
	if err := service.run("systemctl", append([]string{"enable", "--now"}, timers...)...); err != nil {
		return fmt.Errorf("APT 配置已保存，但定时器未全部启用: %w", err)
	}
	for _, name := range timers {
		state, err := service.timer(name)
		if err != nil {
			return err
		}
		if !state.running() {
			return fmt.Errorf("APT 配置已保存，但定时器 %s 尚未启用或启动", name)
		}
	}
	return nil
}
