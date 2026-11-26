package ups

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"snail_tool/internal/ui"
)

func TestNUTServiceFieldsKeepIndependentStates(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	script := `#!/bin/sh
case "$3" in
    nut-driver@ups.service) printf 'LoadState=not-found\nActiveState=inactive\n' ;;
    nut-server.service) printf 'LoadState=loaded\nActiveState=active\nUnitFileState=enabled\n' ;;
    *) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "systemctl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	want := []ui.CardField{
		{Label: "nut-driver@ups", Value: "未找到服务"},
		{Label: "nut-server", Value: "运行中 · enabled"},
		{Label: "nut-monitor", Value: "检测失败"},
	}
	if got := nutServiceFields(); !reflect.DeepEqual(got, want) {
		t.Fatalf("fields = %#v, want %#v", got, want)
	}
}

func TestUPSTelemetryShowsReportedFields(t *testing.T) {
	output := "ups.model: UPS: 850 VA\r\n" +
		"ups.status: OB LB DISCHRG OVER\n" +
		"battery.charge: 12\n" +
		"ups.load: 0\n" +
		"input.voltage: \n" +
		"device.serial: private-serial\n"
	want := []ui.CardField{
		{Label: "设备型号", Value: "UPS: 850 VA"},
		{Label: "供电状态", Value: "电池供电 · 电量低 · 放电中 · OVER"},
		{Label: "电池电量", Value: "12%"},
		{Label: "当前负载", Value: "0%"},
	}
	if got := upsTelemetryFields(output); !reflect.DeepEqual(got, want) {
		t.Fatalf("fields = %#v, want %#v", got, want)
	}
}
