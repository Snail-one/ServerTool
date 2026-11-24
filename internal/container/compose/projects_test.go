package compose

import (
	"reflect"
	"testing"
)

func TestParseComposePSJSON(t *testing.T) {
	raw := `[
		{"Name":"demo-web-1","Project":"demo","Service":"web","State":"running","Status":"Up 2 minutes"},
		{"Name":"demo-worker-1","Project":"demo","Service":"worker","State":"exited","Status":"Exited (0) 1 hour ago"},
		{"Name":"demo-db-1","Project":"demo","Service":"db","State":"paused","Status":"Up 1 hour (Paused)"}
	]`

	got, projectName, err := parseComposePSJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if projectName != "demo" {
		t.Fatalf("project name = %q, want demo", projectName)
	}
	if got.Total != 3 || got.Running != 1 || got.Exited != 1 || got.Paused != 1 {
		t.Fatalf("summary parsed incorrectly: %#v", got)
	}
}

func TestParseComposePSText(t *testing.T) {
	raw := `NAME              IMAGE     COMMAND   SERVICE   CREATED        STATUS                    PORTS
demo-web-1        nginx     "nginx"   web       2 minutes ago  Up 2 minutes              0.0.0.0:80->80/tcp
demo-worker-1     busybox   "sh"      worker    1 hour ago     Exited (0) 1 hour ago`

	got := parseComposePSText(raw)
	if got.Total != 2 || got.Running != 1 || got.Exited != 1 {
		t.Fatalf("summary parsed incorrectly: %#v", got)
	}
}

func TestComposeProjectStatusDisplay(t *testing.T) {
	tests := []struct {
		name    string
		project composeProjectInfo
		want    string
	}{
		{
			name:    "compose ls status wins",
			project: composeProjectInfo{Status: "running(2)", Containers: composeContainerSummary{HasData: true, Total: 2, Running: 2}},
			want:    "running(2)",
		},
		{
			name:    "partial running",
			project: composeProjectInfo{Containers: composeContainerSummary{HasData: true, Total: 3, Running: 1, Exited: 2}},
			want:    "部分运行",
		},
		{
			name:    "stopped",
			project: composeProjectInfo{Containers: composeContainerSummary{HasData: true, Total: 2, Exited: 2}},
			want:    "已停止",
		},
		{
			name:    "empty",
			project: composeProjectInfo{Containers: composeContainerSummary{HasData: true}},
			want:    "无容器",
		},
		{
			name:    "error",
			project: composeProjectInfo{StatusError: "boom"},
			want:    "状态获取失败",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := composeProjectStatusDisplay(tt.project); got != tt.want {
				t.Fatalf("status = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestComposeContainerSummaryDisplay(t *testing.T) {
	got := composeContainerSummaryDisplay(composeContainerSummary{
		HasData: true,
		Total:   4,
		Running: 2,
		Exited:  1,
		Unknown: 1,
	})
	want := "总4 运行2 停止1 未知1"
	if got != want {
		t.Fatalf("display = %q, want %q", got, want)
	}
}

func TestComposeProjectActionForChoice(t *testing.T) {
	tests := []struct {
		choice string
		wantOK bool
		want   composeProjectAction
	}{
		{choice: "1", wantOK: true, want: composeProjectAction{Name: "up -d", Args: []string{"up", "-d"}}},
		{choice: "2", wantOK: true, want: composeProjectAction{Name: "stop", Args: []string{"stop"}}},
		{choice: "3", wantOK: true, want: composeProjectAction{Name: "restart", Args: []string{"restart"}}},
		{choice: "4", wantOK: true, want: composeProjectAction{Name: "down", Args: []string{"down"}}},
		{choice: "x", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.choice, func(t *testing.T) {
			got, ok := composeProjectActionForChoice(tt.choice)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("action = %#v, want %#v", got, tt.want)
			}
		})
	}
}
