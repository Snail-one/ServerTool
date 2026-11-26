package ups

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

	"snail_tool/internal/system"
	"snail_tool/internal/system/packagemanager"
	"snail_tool/internal/ui"
)

func printUPSStatus() {
	installed := isNUTInstalled()
	backups := "无"
	if count := len(existingNUTBackups()); count > 0 {
		backups = fmt.Sprintf("%d 个文件", count)
	}
	fields := []ui.CardField{
		{Label: "NUT", Value: ui.InstallationBadge(installed)},
		{Label: "ServerTool 配置", Value: ui.ConfiguredBadge(IsUPSConfigured())},
		{Label: "首次备份", Value: backups},
	}
	if installed {
		fields = append(fields, nutServiceFields()...)
	}
	fields = append(fields, localUPSFields()...)
	ui.PrintInfoCard("当前 UPS 状态", fields...)
}

func nutServiceFields() []ui.CardField {
	if !system.CommandExists("systemctl") {
		return []ui.CardField{{Label: "NUT 服务", Value: "无法检测", Detail: "未找到 systemctl"}}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var fields []ui.CardField
	for _, service := range nutSystemdServices() {
		output, err := packagemanager.QueryOutputContext(ctx, "systemctl", "show", "--property=LoadState,ActiveState,UnitFileState", service.unit+".service")
		field := ui.CardField{Label: service.unit, Value: "检测失败"}
		if err == nil {
			state := statusValues(output, "=")
			switch state["LoadState"] {
			case "not-found":
				field.Value = "未找到服务"
			case "masked":
				field.Value = "已屏蔽"
			case "loaded":
				field.Value = state["ActiveState"]
				switch field.Value {
				case "active":
					field.Value = "运行中"
				case "inactive":
					field.Value = "未运行"
				case "failed":
					field.Value = "运行失败"
				case "":
					field.Value = "状态未知"
				}
				if unitState := state["UnitFileState"]; unitState != "" {
					field.Value += " · " + unitState
				}
			}
		} else if ctx.Err() != nil {
			field.Value = "查询超时"
		}
		fields = append(fields, field)
	}
	return fields
}

func localUPSFields() []ui.CardField {
	config, err := os.ReadFile(upsConfPath)
	if err != nil && !os.IsNotExist(err) {
		return []ui.CardField{{Label: "本机 UPS", Value: "配置读取失败", Detail: upsConfPath}}
	}
	if !hasNUTSection(string(config), upsMonitorName) {
		return []ui.CardField{{Label: "本机 UPS", Value: "未找到 [" + upsMonitorName + "] 设备配置"}}
	}
	target := upsMonitorName + "@localhost"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	output, err := packagemanager.QueryOutputContext(ctx, "upsc", target)
	field := ui.CardField{Label: "本机 UPS", Value: target + " · 已连接"}
	if err != nil {
		field.Value = target + " · 读取失败"
		if ctx.Err() != nil {
			field.Value = target + " · 查询超时"
		} else if !system.CommandExists("upsc") {
			field.Detail = "未找到 upsc"
		} else {
			field.Detail = "请检查 USB 连接和 NUT 服务日志"
		}
		return []ui.CardField{field}
	}
	return append([]ui.CardField{field}, upsTelemetryFields(output)...)
}

func upsTelemetryFields(output string) []ui.CardField {
	values := statusValues(output, ":")
	model := values["device.model"]
	if model == "" {
		model = values["ups.model"]
	}
	var fields []ui.CardField
	if model != "" {
		fields = append(fields, ui.CardField{Label: "设备型号", Value: model})
	}
	if status := values["ups.status"]; status != "" {
		labels := map[string]string{"OL": "市电供电", "OB": "电池供电", "LB": "电量低", "CHRG": "充电中", "DISCHRG": "放电中", "FSD": "强制关机"}
		states := strings.Fields(status)
		for i, state := range states {
			if label := labels[state]; label != "" {
				states[i] = label
			}
		}
		fields = append(fields, ui.CardField{Label: "供电状态", Value: strings.Join(states, " · ")})
	}
	for _, metric := range []struct{ key, label, unit string }{
		{"battery.charge", "电池电量", "%"},
		{"battery.runtime", "预计续航", " 秒"},
		{"ups.load", "当前负载", "%"},
		{"input.voltage", "输入电压", " V"},
	} {
		if value := values[metric.key]; value != "" {
			fields = append(fields, ui.CardField{Label: metric.label, Value: value + metric.unit})
		}
	}
	return fields
}

func statusValues(output, separator string) map[string]string {
	values := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		if key, value, ok := strings.Cut(line, separator); ok {
			value = strings.Map(func(r rune) rune {
				if unicode.IsControl(r) {
					return -1
				}
				return r
			}, value)
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return values
}
