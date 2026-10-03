package golang

import (
	"snail_tool/internal/ui"
	"testing"
)

func TestGoStatusFieldsShowsThemedInstallPath(t *testing.T) {
	fields := goStatusFields("go1.24.1", 2, false)
	if len(fields) != 3 {
		t.Fatalf("status field count = %d, want 3: %#v", len(fields), fields)
	}
	if fields[0].Label != "当前版本" || fields[0].Value != ui.Badge("go1.24.1", true) {
		t.Fatalf("current version field = %#v", fields[0])
	}
	if fields[1].Label != "安装位置" || fields[1].Value != "/opt/go/go1.24.1" || fields[1].Detail != "" {
		t.Fatalf("install path should be a themed card value, got %#v", fields[1])
	}
	if fields[2].Label != "已安装版本" || fields[2].Value != "2 个" {
		t.Fatalf("installed count field = %#v", fields[2])
	}
}

func TestGoStatusFieldsOmitsInstallPathWhenUnconfigured(t *testing.T) {
	fields := goStatusFields("", 0, true)
	for _, field := range fields {
		if field.Label == "安装位置" {
			t.Fatalf("unconfigured status should not include install path: %#v", fields)
		}
	}
	last := fields[len(fields)-1]
	if last.Label != "官方位置 Go" || last.Value != ui.StatusBadge("需确认") {
		t.Fatalf("official field = %#v", last)
	}
}
