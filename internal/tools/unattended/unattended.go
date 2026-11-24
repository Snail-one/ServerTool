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
	periodic, refresh, download, upgrade, reboot, preferred string
	shutdown, preferredShutdown                             string
}

func (service manager) settings() (settings, error) {
	output, err := service.query("apt-config", "shell",
		"PERIODIC", "APT::Periodic::Enable", "REFRESH", "APT::Periodic::Update-Package-Lists",
		"DOWNLOAD", downloadKey, "UPGRADE", upgradeKey, "REBOOT", rebootKey+"/b", "PREFERRED", intervalKey,
		"SHUTDOWN", shutdownKey+"/b", "PREFERRED_SHUTDOWN", shutdownPreference)
	if err != nil {
		return settings{}, fmt.Errorf("读取 APT 生效设置失败: %w", err)
	}
	values := assignments(output)
	return settings{periodic: values["PERIODIC"], refresh: values["REFRESH"], download: values["DOWNLOAD"], upgrade: values["UPGRADE"],
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
	load, file, active, next              string
	calendar, delay, accuracy, persistent string
}

func (service manager) timer(name string) (timer, error) {
	if !service.exists("systemctl") {
		return timer{}, fmt.Errorf("未找到 systemctl")
	}
	output, err := service.query("systemctl", "show", "--property=LoadState,UnitFileState,ActiveState,NextElapseUSecRealtime,TimersCalendar,RandomizedDelayUSec,AccuracyUSec,Persistent", name)
	if err != nil {
		return timer{}, fmt.Errorf("查询 %s 失败: %w", name, err)
	}
	values := assignments(output)
	return timer{
		load: values["LoadState"], file: values["UnitFileState"], active: values["ActiveState"], next: values["NextElapseUSecRealtime"],
		calendar: values["TimersCalendar"], delay: values["RandomizedDelayUSec"],
		accuracy: values["AccuracyUSec"], persistent: values["Persistent"],
	}, nil
}

func (state timer) running() bool {
	return state.file == "enabled" && state.active == "active"
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
	title := "关闭自动更新"
	if enable {
		title = "开启自动更新"
	}
	showPage(title)
	ui.PrintInfoCard("应用范围",
		ui.CardField{Label: "操作", Value: message},
		ui.CardField{Label: "配置文件", Value: service.path},
	)
	fmt.Println()
	confirmed, err := view.Confirm("确认执行？(y/N)：")
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
