package container

import (
	"errors"
	"os"
	"testing"

	"snail_tool/internal/container/podman"
	"snail_tool/internal/ui"
)

func TestManageRefreshesAfterInstallAndUninstall(t *testing.T) {
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	// Install Podman, try the blocked Docker option, then enter Podman and uninstall it.
	if _, err := writer.WriteString("2\n\n1\n\n2\nq\n"); err != nil {
		_ = writer.Close()
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	stdin := os.Stdin
	os.Stdin = input
	view := ui.New()
	os.Stdin = stdin

	probes := 0
	var installed []Runtime
	err = manage(view, func() []Runtime {
		probes++
		return installed
	}, func(_ *ui.UI, selected Runtime) error {
		if selected.Name != "podman" {
			t.Fatalf("selected %q, want podman", selected.Name)
		}
		installed = nil
		return podman.ErrBackToRuntimes
	}, func(_ *ui.UI, name string) error {
		if name != "podman" || len(installed) != 0 {
			t.Fatalf("unexpected install of %q with runtimes %v", name, installed)
		}
		installed = []Runtime{{Name: "podman", Display: "Podman"}}
		return nil
	})
	if !errors.Is(err, ui.ErrReturnToMenu) || probes != 3 || len(installed) != 0 {
		t.Fatalf("end state: probes=%d, installed=%v, error=%v", probes, installed, err)
	}
}

func TestRuntimeStatusFieldsIncludeDockerLogAndProxy(t *testing.T) {
	fields := runtimeStatusFields(
		[]Runtime{{Name: "docker", Display: "Docker"}},
		"json-file，100m × 3",
		"http://admin:******@127.0.0.1:7890/",
		"HTTP http://192.168.1.100:7890",
	)
	if len(fields) != 4 {
		t.Fatalf("fields = %#v", fields)
	}
	if fields[0].Label != "当前运行时" || fields[0].Value != "Docker" {
		t.Fatalf("runtime field = %#v", fields[0])
	}
	if fields[1].Label != "容器日志" || fields[1].Value != "json-file，100m × 3" {
		t.Fatalf("log field = %#v", fields[1])
	}
	if fields[2].Label != "服务代理" || fields[2].Value != "http://admin:******@127.0.0.1:7890/" {
		t.Fatalf("proxy field = %#v", fields[2])
	}
	if fields[3].Label != "构建与容器代理" || fields[3].Value != "HTTP http://192.168.1.100:7890" {
		t.Fatalf("client proxy field = %#v", fields[3])
	}
}

func TestRuntimeByChoiceSelectsDockerOrPodman(t *testing.T) {
	name, ok := runtimeByChoice("1")
	if !ok || name != "docker" {
		t.Fatalf("choice 1 = %q ok=%v", name, ok)
	}
	name, ok = runtimeByChoice("2")
	if !ok || name != "podman" {
		t.Fatalf("choice 2 = %q ok=%v", name, ok)
	}
	if _, ok := runtimeByChoice("3"); ok {
		t.Fatal("choice 3 was accepted")
	}
}

func TestRuntimeStatusFieldsOmitDockerDetailsWithoutDocker(t *testing.T) {
	fields := runtimeStatusFields(
		[]Runtime{{Name: "podman", Display: "Podman"}},
		"不应显示",
		"不应显示",
		"不应显示",
	)
	if len(fields) != 1 || fields[0].Label != "当前运行时" || fields[0].Value != "Podman" {
		t.Fatalf("fields = %#v", fields)
	}
}

func TestRuntimeSummary(t *testing.T) {
	tests := []struct {
		name string
		in   []Runtime
		want string
	}{
		{name: "none", want: "未安装"},
		{name: "docker", in: []Runtime{{Name: "docker", Display: "Docker"}}, want: "Docker"},
		{name: "docker abnormal", in: []Runtime{{Name: "docker", Display: "Docker（服务异常）"}}, want: "Docker（服务异常）"},
		{name: "podman", in: []Runtime{{Name: "podman", Display: "Podman"}}, want: "Podman"},
		{name: "both", in: []Runtime{{Name: "docker", Display: "Docker"}, {Name: "podman", Display: "Podman"}}, want: "Docker、Podman"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RuntimeSummary(tt.in); got != tt.want {
				t.Fatalf("summary = %q, want %q", got, tt.want)
			}
		})
	}
}
