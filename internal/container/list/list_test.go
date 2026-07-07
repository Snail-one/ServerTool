package list

import (
	"reflect"
	"testing"
)

func TestParseContainersJSONLines(t *testing.T) {
	raw := `{"ID":"abcdef1234567890","Names":"web","Ports":"0.0.0.0:8080->80/tcp","Status":"Up 3 minutes","State":"running","CreatedAt":"2026-07-07 10:00:00 +0800 CST","RunningFor":"3 minutes"}` + "\n" +
		`{"ID":"123456abcdef","Names":"worker","Ports":"","Status":"Exited (0) 2 hours ago","State":"exited","CreatedAt":"2026-07-06 10:00:00 +0800 CST","RunningFor":"2 hours ago"}`

	got, err := parseContainersJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("container count = %d, want 2", len(got))
	}

	if got[0].Name != "web" || got[0].State != containerStateRunning || !got[0].IsRunning {
		t.Fatalf("running container parsed incorrectly: %#v", got[0])
	}
	if got[1].Name != "worker" || got[1].State != containerStateExited || got[1].IsRunning {
		t.Fatalf("exited container parsed incorrectly: %#v", got[1])
	}
}

func TestParseContainersJSONArray(t *testing.T) {
	raw := `[{"Id":"podman123","Names":["db"],"Ports":[{"host_ip":"0.0.0.0","host_port":5432,"container_port":5432,"protocol":"tcp"}],"Status":"Up 1 hour","State":"running"}]`

	got, err := parseContainersJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("container count = %d, want 1", len(got))
	}

	wantPorts := "0.0.0.0:5432->5432/tcp"
	if got[0].ID != "podman123" || got[0].Name != "db" || got[0].Ports != wantPorts {
		t.Fatalf("json array parsed incorrectly: %#v", got[0])
	}
}

func TestParseContainersText(t *testing.T) {
	raw := "abcdef1234567890\tapi\t\tExited (0) 1 hour ago\t2026-07-07 10:00:00 +0800 CST\t1 hour ago\n"

	got := parseContainersText(raw)
	if len(got) != 1 {
		t.Fatalf("container count = %d, want 1", len(got))
	}
	if got[0].Name != "api" || got[0].State != containerStateExited || got[0].IsRunning {
		t.Fatalf("text container parsed incorrectly: %#v", got[0])
	}
}

func TestNormalizeContainerState(t *testing.T) {
	tests := []struct {
		name   string
		state  string
		status string
		want   string
	}{
		{name: "explicit running", state: "running", status: "Up 1 hour", want: containerStateRunning},
		{name: "paused from status", status: "Up 1 hour (Paused)", want: containerStatePaused},
		{name: "restarting from status", status: "Restarting (1) 3 seconds ago", want: containerStateRestarting},
		{name: "exited from status", status: "Exited (0) 2 hours ago", want: containerStateExited},
		{name: "created from status", status: "Created", want: containerStateCreated},
		{name: "unknown", status: "Something else", want: containerStateUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeContainerState(tt.state, tt.status); got != tt.want {
				t.Fatalf("state = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestContainerActionAvailability(t *testing.T) {
	tests := []struct {
		name      string
		state     string
		wantStart bool
		wantStop  bool
	}{
		{name: "running", state: containerStateRunning, wantStop: true},
		{name: "paused", state: containerStatePaused, wantStop: true},
		{name: "restarting", state: containerStateRestarting, wantStop: true},
		{name: "exited", state: containerStateExited, wantStart: true},
		{name: "created", state: containerStateCreated, wantStart: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := containerInfo{State: tt.state}
			if got := canStartContainer(c); got != tt.wantStart {
				t.Fatalf("canStart = %v, want %v", got, tt.wantStart)
			}
			if got := canStopContainer(c); got != tt.wantStop {
				t.Fatalf("canStop = %v, want %v", got, tt.wantStop)
			}
		})
	}
}

func TestContainerCommandArgs(t *testing.T) {
	tests := []struct {
		name string
		got  []string
		want []string
	}{
		{name: "start", got: containerLifecycleArgs("start", "web"), want: []string{"start", "web"}},
		{name: "logs", got: containerLogsArgs("web", false), want: []string{"logs", "--tail", "200", "web"}},
		{name: "follow logs", got: containerLogsArgs("web", true), want: []string{"logs", "-f", "--tail", "100", "web"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !reflect.DeepEqual(tt.got, tt.want) {
				t.Fatalf("args = %#v, want %#v", tt.got, tt.want)
			}
		})
	}
}

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
