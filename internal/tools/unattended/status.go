package unattended

import (
	"strings"
	"time"

	"snail_tool/internal/ui"
)

func showPage(title string) {
	ui.ClearScreen()
	ui.MenuTitle("系统工具", Title, title)
}

func (service manager) overview() (policy, timers []ui.CardField, installed bool) {
	installed = service.exists("unattended-upgrade")
	policy = []ui.CardField{{Label: "软件包", Value: ui.InstallationBadge(installed)}}
	current, err := service.settings()
	if err != nil {
		policy = append(policy, ui.CardField{Label: "APT 策略", Value: "检测失败", Detail: err.Error()})
	} else {
		policy = append(policy, current.policyFields(installed)...)
	}
	for _, name := range []string{refreshTimer, upgradeTimer} {
		state, err := service.timer(name)
		field := ui.CardField{Label: timerLabel(name), Value: state.statusLabel()}
		if err != nil {
			field.Value, field.Detail = "检测失败", err.Error()
		} else {
			field.Detail = "下次触发：" + triggerLabel(state.next)
		}
		timers = append(timers, field)
	}
	return policy, timers, installed
}

func (current settings) policyFields(installed bool) []ui.CardField {
	periodic := "开启"
	if current.periodic == "0" {
		periodic = "关闭"
	}
	upgrade := ui.CardField{Label: "自动安装更新", Value: intervalLabel(current.upgrade)}
	switch {
	case activeInterval(current.upgrade) && !installed:
		upgrade.Detail = "策略已配置，等待安装 unattended-upgrades"
	case activeInterval(current.upgrade) && current.periodic == "0":
		upgrade.Detail = "APT 定时任务总开关已关闭"
	case !activeInterval(current.upgrade) && activeInterval(current.preferred):
		upgrade.Detail = "保存的更新频率：" + intervalLabel(current.preferred)
	}
	return []ui.CardField{
		{Label: "APT 定时任务", Value: periodic},
		{Label: "刷新软件包索引", Value: intervalLabel(current.refresh)},
		{Label: "预下载软件包", Value: intervalLabel(current.download)},
		upgrade,
		{Label: "自动重启", Value: switchLabel(current.reboot)},
		{Label: "关机时安装", Value: switchLabel(current.shutdown)},
	}
}

func switchLabel(value string) string {
	switch strings.ToLower(value) {
	case "true", "yes", "on", "1":
		return "开启"
	case "", "false", "no", "off", "0":
		return "关闭"
	default:
		return value
	}
}

func timerLabel(name string) string {
	if name == refreshTimer {
		return "索引刷新"
	}
	return "自动更新"
}

func (state timer) statusLabel() string {
	switch state.load {
	case "not-found":
		return "未找到"
	case "masked":
		return "已屏蔽"
	case "":
		return "未知"
	case "loaded":
	default:
		return state.load
	}

	startup := state.file
	switch state.file {
	case "enabled":
		startup = "已启用"
	case "enabled-runtime":
		startup = "临时启用"
	case "disabled":
		startup = "未启用"
	case "masked", "masked-runtime":
		startup = "已屏蔽"
	case "static":
		startup = "静态单元"
	case "":
		startup = "启用状态未知"
	}
	runtime := state.active
	switch state.active {
	case "active":
		runtime = "已启动"
	case "inactive":
		runtime = "未启动"
	case "failed":
		runtime = "运行失败"
	case "activating":
		runtime = "正在启动"
	case "deactivating":
		runtime = "正在停止"
	case "":
		runtime = "运行状态未知"
	}
	return startup + " · " + runtime
}

func triggerLabel(value string) string {
	if value == "" || value == "n/a" {
		return "暂无"
	}
	if parsed, err := time.Parse("Mon 2006-01-02 15:04:05 MST", value); err == nil {
		return parsed.Format("2006-01-02 15:04:05 MST")
	}
	return value
}
