package unattended

import (
	"strings"
	"time"

	"snail_tool/internal/system"
	"snail_tool/internal/system/packagemanager"
	"snail_tool/internal/ui"
)

// StatusField reports the update summary and whether updates are active.
func StatusField() (ui.CardField, bool) {
	service := manager{exists: system.CommandExists, query: packagemanager.QueryOutput}
	return service.statusField()
}

func (service manager) statusField() (ui.CardField, bool) {
	field := ui.CardField{Label: Title}
	switch {
	case !service.exists("apt-get") || !service.exists("apt-config"):
		field.Value = "当前系统不支持 APT 更新管理"
		return field, false
	case !service.exists("unattended-upgrade"):
		field.Value = "未安装"
		return field, false
	}
	current, err := service.settings()
	if err != nil {
		field.Value = "APT 策略检测失败"
		return field, false
	}
	if current.shutdown == "true" {
		field.Value = "已配置关机时安装"
		return field, true
	}
	var state timer
	if current.periodic != "0" && activeInterval(current.upgrade) {
		state, err = service.timer(upgradeTimer)
	}
	field, ready := current.scheduledInstallStatus(true, state, err)
	field.Label = Title
	if ready {
		field.Detail = "下次定时器唤醒：" + triggerLabel(state.next)
	}
	return field, ready
}

func showPage(title string) {
	ui.ClearScreen()
	ui.MenuTitle("系统工具", Title, title)
}

func (service manager) overview(current settings, settingsErr error, installed bool) (policy, timers []ui.CardField) {
	policy = []ui.CardField{{Label: "软件包", Value: ui.InstallationBadge(installed)}}
	refresh, refreshErr := service.timer(refreshTimer)
	upgrade, upgradeErr := service.timer(upgradeTimer)
	refreshStatus, upgradeStatus := "无法确认 · APT 策略检测失败", "无法确认 · APT 策略检测失败"
	if settingsErr != nil {
		policy = append(policy, ui.CardField{Label: "APT 策略", Value: "检测失败", Detail: settingsErr.Error()})
	} else {
		installStatus, _ := current.scheduledInstallStatus(installed, upgrade, upgradeErr)
		policy = append(policy, current.policyFields(installStatus)...)
		refreshStatus = current.scheduledRefreshStatus(refresh, refreshErr)
		upgradeStatus = installStatus.Value
		if !activeInterval(current.upgrade) {
			upgradeStatus = "未生效 · 定时安装策略未开启"
		}
	}
	return policy, []ui.CardField{
		refresh.overviewField(refreshTimer, refreshStatus, refreshErr),
		upgrade.overviewField(upgradeTimer, upgradeStatus, upgradeErr),
	}
}

func (state timer) overviewField(name, taskStatus string, err error) ui.CardField {
	field := ui.CardField{Label: timerLabel(name), Value: taskStatus}
	if err != nil {
		field.Detail = "定时器检测失败：" + err.Error()
	} else {
		field.Detail = "定时器：" + state.statusLabel() + " · 下次唤醒：" + triggerLabel(state.next)
	}
	return field
}

func (state timer) schedulingBlocker(timerErr error) string {
	switch {
	case timerErr != nil:
		return "无法确认 · 定时器检测失败"
	case state.load != "loaded" || state.active != "active":
		return "未生效 · 定时器未运行"
	case triggerLabel(state.next) == "暂无":
		return "未生效 · 定时器暂无下次唤醒时间"
	}
	return ""
}

func (current settings) scheduledRefreshStatus(state timer, timerErr error) string {
	if !activeInterval(current.refresh) {
		return "未生效 · 索引刷新策略未开启"
	}
	if current.periodic == "0" {
		return "未生效 · APT 周期任务总开关已关闭"
	}
	if blocker := state.schedulingBlocker(timerErr); blocker != "" {
		return blocker
	}
	return "调度已生效 · " + intervalLabel(current.refresh)
}

func (current settings) scheduledInstallStatus(installed bool, state timer, timerErr error) (ui.CardField, bool) {
	field := ui.CardField{Label: "定时自动安装"}
	frequency := intervalLabel(current.upgrade)
	switch {
	case !activeInterval(current.upgrade):
		field.Value = frequency
		if activeInterval(current.preferred) {
			field.Detail = "保存的更新频率：" + intervalLabel(current.preferred)
		}
	case !installed:
		field.Value, field.Detail = "未生效 · 软件包未安装", "配置频率："+frequency
	case current.periodic == "0":
		field.Value, field.Detail = "未生效 · APT 周期任务总开关已关闭", "配置频率："+frequency
	case current.shutdown == "true":
		field.Value = "未生效 · 已配置关机安装"
	default:
		if blocker := state.schedulingBlocker(timerErr); blocker != "" {
			field.Value, field.Detail = blocker, "配置频率："+frequency
			break
		}
		field.Value = "调度已生效 · " + frequency
		return field, true
	}
	return field, false
}

func (current settings) policyFields(installStatus ui.CardField) []ui.CardField {
	periodic := "开启"
	switch current.periodic {
	case "":
		periodic = "开启（默认）"
	case "0":
		periodic = "关闭"
	}
	return []ui.CardField{
		{Label: "APT 周期任务总开关", Value: periodic},
		{Label: "刷新软件包索引", Value: intervalLabel(current.refresh)},
		{Label: "预下载软件包", Value: intervalLabel(current.download)},
		installStatus,
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
