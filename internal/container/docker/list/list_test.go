package list

import (
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/container/docker/listing"
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

	if got[0].Name != "web" || got[0].State != listing.StateRunning || !got[0].IsRunning {
		t.Fatalf("running container parsed incorrectly: %#v", got[0])
	}
	if got[1].Name != "worker" || got[1].State != listing.StateExited || got[1].IsRunning {
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
	if got[0].Name != "api" || got[0].State != listing.StateExited || got[0].IsRunning {
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
		{name: "explicit running", state: "running", status: "Up 1 hour", want: listing.StateRunning},
		{name: "paused from status", status: "Up 1 hour (Paused)", want: listing.StatePaused},
		{name: "restarting from status", status: "Restarting (1) 3 seconds ago", want: listing.StateRestarting},
		{name: "exited from status", status: "Exited (0) 2 hours ago", want: listing.StateExited},
		{name: "created from status", status: "Created", want: listing.StateCreated},
		{name: "dead from status", status: "Dead", want: listing.StateDead},
		{name: "unknown", status: "Something else", want: listing.StateUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := listing.NormalizeContainerState(tt.state, tt.status); got != tt.want {
				t.Fatalf("state = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestContainerActionAvailability(t *testing.T) {
	tests := []struct {
		name       string
		state      string
		wantStart  bool
		wantStop   bool
		wantEnter  bool
		wantRemove bool
		wantPause  string
	}{
		{name: "running", state: listing.StateRunning, wantStop: true, wantEnter: true, wantPause: "pause"},
		{name: "paused", state: listing.StatePaused, wantStop: true, wantPause: "unpause"},
		{name: "restarting", state: listing.StateRestarting, wantStop: true},
		{name: "exited", state: listing.StateExited, wantStart: true, wantRemove: true},
		{name: "created", state: listing.StateCreated, wantStart: true, wantRemove: true},
		{name: "dead", state: listing.StateDead, wantStart: true, wantRemove: true},
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
			if got := canEnterContainer(c); got != tt.wantEnter {
				t.Fatalf("canEnter = %v, want %v", got, tt.wantEnter)
			}
			if got := canRemoveContainer(c); got != tt.wantRemove {
				t.Fatalf("canRemove = %v, want %v", got, tt.wantRemove)
			}
			keys := make(map[string]bool)
			for _, action := range availableContainerActions(c, false) {
				keys[action.Key] = true
			}
			if tt.wantPause != "" && !keys[tt.wantPause] {
				t.Fatalf("actions = %#v, want %s", keys, tt.wantPause)
			}
			if tt.wantPause == "" && (keys["pause"] || keys["unpause"]) {
				t.Fatalf("actions = %#v, did not want pause action", keys)
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
		{name: "shell", got: containerShellArgs("web"), want: []string{"exec", "-it", "web", "sh", "-lc", "if command -v bash >/dev/null 2>&1; then bash; else sh; fi; exit 0"}},
		{name: "inspect", got: containerInspectArgs("web"), want: []string{"inspect", "web"}},
		{name: "logs", got: containerLogsArgs("web", false), want: []string{"logs", "--tail", "200", "web"}},
		{name: "follow logs", got: containerLogsArgs("web", true), want: []string{"logs", "-f", "--tail", "100", "web"}},
		{name: "pause", got: containerLifecycleArgs("pause", "web"), want: []string{"pause", "web"}},
		{name: "unpause", got: containerLifecycleArgs("unpause", "web"), want: []string{"unpause", "web"}},
		{name: "rm", got: containerRemoveArgs("web"), want: []string{"rm", "web"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !reflect.DeepEqual(tt.got, tt.want) {
				t.Fatalf("args = %#v, want %#v", tt.got, tt.want)
			}
		})
	}
}

func TestRemoveContainerSafety(t *testing.T) {
	c := containerInfo{ID: "abcdef", Name: "web", State: listing.StateExited}
	runs := 0
	executed, err := removeContainer(
		func(prompt string) (bool, error) {
			if !strings.Contains(prompt, "web") || !strings.Contains(prompt, "abcdef") {
				t.Fatalf("confirmation omits identity: %q", prompt)
			}
			return false, nil
		},
		func(string, ...string) error { runs++; return nil },
		"docker",
		c,
	)
	if err != nil || executed || runs != 0 {
		t.Fatalf("cancel result: executed=%v runs=%d err=%v", executed, runs, err)
	}

	c.State = listing.StateRunning
	confirmed := 0
	_, err = removeContainer(
		func(string) (bool, error) { confirmed++; return true, nil },
		func(string, ...string) error { runs++; return nil },
		"docker",
		c,
	)
	if err == nil || confirmed != 0 || runs != 0 {
		t.Fatalf("running rm was not blocked: confirmed=%d runs=%d err=%v", confirmed, runs, err)
	}
	c.State = listing.StateExited
	executed, err = removeContainer(func(string) (bool, error) { return true, nil }, func(name string, args ...string) error {
		if name != "docker" || !reflect.DeepEqual(args, []string{"rm", "abcdef"}) {
			t.Fatalf("deletion must use stable ID: %s %v", name, args)
		}
		return nil
	}, "docker", c)
	if err != nil || !executed {
		t.Fatal("stopped container deletion failed", err)
	}
}
