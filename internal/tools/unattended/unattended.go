package unattended

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"snail_tool/internal/storage"
	"snail_tool/internal/system"
	"snail_tool/internal/system/packagemanager"
	"snail_tool/internal/ui"
)

const (
	configPath    = "/etc/apt/apt.conf.d/99-z-servertool-unattended-upgrades.conf"
	refreshTimer  = "apt-daily.timer"
	upgradeTimer  = "apt-daily-upgrade.timer"
	managedHeader = "// Managed by ServerTool.\n"
	enabledConfig = managedHeader + `APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
Unattended-Upgrade::Automatic-Reboot "false";
`
	disabledConfig = managedHeader + "APT::Periodic::Unattended-Upgrade \"0\";\n"
)

type manager struct {
	path   string
	exists func(string) bool
	root   func() bool
	query  func(string, ...string) (string, error)
	run    func(string, ...string) error
}

func Run(view *ui.UI) error {
	service := manager{configPath, system.CommandExists, system.IsRoot, packagemanager.QueryOutput, system.Run}
	if !service.exists("apt-get") || !service.exists("apt-config") {
		return fmt.Errorf("此功能需要 APT 和 unattended-upgrades")
	}
	return service.menu(view)
}

type settings struct {
	periodic, refresh, upgrade, reboot string
}

func (service manager) settings() (settings, error) {
	output, err := service.query("apt-config", "shell",
		"PERIODIC", "APT::Periodic::Enable", "REFRESH", "APT::Periodic::Update-Package-Lists",
		"UPGRADE", "APT::Periodic::Unattended-Upgrade", "REBOOT", "Unattended-Upgrade::Automatic-Reboot/b")
	if err != nil {
		return settings{}, fmt.Errorf("读取 APT 生效设置失败: %w", err)
	}
	values := assignments(output)
	return settings{values["PERIODIC"], values["REFRESH"], values["UPGRADE"], values["REBOOT"]}, nil
}

func assignments(output string) map[string]string {
	values := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			values[key] = strings.Trim(value, "'")
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
	load, file, active string
}

func (service manager) timer(name string) (timer, error) {
	if !service.exists("systemctl") {
		return timer{}, fmt.Errorf("未找到 systemctl")
	}
	output, err := service.query("systemctl", "show", "--property=LoadState,UnitFileState,ActiveState", name)
	if err != nil {
		return timer{}, fmt.Errorf("查询 %s 失败: %w", name, err)
	}
	values := assignments(output)
	return timer{values["LoadState"], values["UnitFileState"], values["ActiveState"]}, nil
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
		if current.periodic == "0" {
			fields = append(fields, ui.CardField{Label: "APT 定时任务", Value: "全局已关闭"})
		}
		fields = append(fields,
			ui.CardField{Label: "刷新软件包索引", Value: intervalLabel(current.refresh)},
			ui.CardField{Label: "自动安装更新", Value: intervalLabel(current.upgrade)},
			ui.CardField{Label: "自动重启", Value: reboot},
		)
	}
	for _, name := range []string{refreshTimer, upgradeTimer} {
		state, err := service.timer(name)
		field := ui.CardField{Label: name, Value: state.file + " / " + state.active}
		if err != nil {
			field.Value, field.Detail = "检测失败", err.Error()
		} else if state.load != "loaded" {
			field.Value = state.load
		}
		fields = append(fields, field)
	}
	return fields
}

func (service manager) change(view *ui.UI, enable bool) error {
	if !service.root() {
		return fmt.Errorf("修改自动更新需要 root 权限")
	}
	previous, exists, err := storage.ReadRegularFile(service.path)
	if err != nil {
		return err
	}
	if exists && string(previous) != enabledConfig && string(previous) != disabledConfig {
		return fmt.Errorf("无法识别现有配置，请先检查 %s", service.path)
	}
	message := "关闭自动安装更新，保留 APT 定时器及其他配置"
	if enable {
		message = "每天检查并按现有软件源策略自动安装更新，关闭自动重启"
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
	content, expected := disabledConfig, "0"
	if enable {
		current, err := service.settings()
		if err != nil {
			return err
		}
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
		content, expected = enabledConfig, "1"
	}

	current, present, err := storage.ReadRegularFile(service.path)
	if err != nil {
		return err
	}
	if present != exists || !bytes.Equal(current, previous) {
		return fmt.Errorf("配置已变化，请重新确认: %s", service.path)
	}
	options := storage.AtomicWriteOptions{Mode: 0644}
	if string(previous) != content {
		if err := storage.AtomicWriteFile(service.path, []byte(content), options); err != nil {
			return err
		}
	}
	effective, err := service.settings()
	if err == nil && (effective.upgrade != expected || enable && (effective.periodic == "0" || effective.refresh != "1" || effective.reboot != "false" && effective.reboot != "0")) {
		err = fmt.Errorf("APT 生效设置与预期不一致，可能有其他配置覆盖 %s", service.path)
	}
	if err != nil {
		if string(previous) == content {
			return err
		}
		return errors.Join(err, service.restore(content, previous, exists, options))
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

func (service manager) restore(written string, previous []byte, existed bool, options storage.AtomicWriteOptions) error {
	current, exists, err := storage.ReadRegularFile(service.path)
	if err != nil {
		return fmt.Errorf("检查回退目标失败: %w", err)
	}
	if !exists || string(current) != written {
		return fmt.Errorf("配置已被其他操作修改，无法回退 %s", service.path)
	}
	if existed {
		err = storage.AtomicWriteFile(service.path, previous, options)
	} else {
		err = storage.RemoveRegularFile(service.path)
	}
	if err != nil {
		return fmt.Errorf("回退 APT 配置失败: %w", err)
	}
	return nil
}
