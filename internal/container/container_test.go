package container

import (
	"testing"

	"snail_tool/internal/container/runtime"
)

func TestRuntimeStatusFieldsIncludeDockerLogAndProxy(t *testing.T) {
	fields := runtimeStatusFields(
		[]runtime.Runtime{{Name: "docker", Display: "Docker"}},
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

func TestUninstallMenuLabelPutsDockerFirst(t *testing.T) {
	docker := []runtime.Runtime{{Name: "docker", Display: "Docker"}}
	if got := uninstallMenuLabel(docker); got != "Docker 卸载" {
		t.Fatalf("docker label = %q", got)
	}
	abnormal := []runtime.Runtime{{Name: "docker", Display: "Docker（服务异常：daemon 不可达）"}}
	if got := uninstallMenuLabel(abnormal); got != "Docker 卸载（服务异常：daemon 不可达）" {
		t.Fatalf("abnormal docker label = %q", got)
	}
	podman := []runtime.Runtime{{Name: "podman", Display: "Podman"}}
	if got := uninstallMenuLabel(podman); got != "Podman 卸载" {
		t.Fatalf("podman label = %q", got)
	}
	both := []runtime.Runtime{{Name: "docker", Display: "Docker"}, {Name: "podman", Display: "Podman"}}
	if got := uninstallMenuLabel(both); got != "卸载 Docker 或 Podman" {
		t.Fatalf("both label = %q", got)
	}
}

func TestContainerMenuLabelsFollowInstalledRuntime(t *testing.T) {
	docker := containerMenuLabels([]runtime.Runtime{{Name: "docker", Display: "Docker"}})
	if docker.manage != "Docker 管理" || docker.config != "Docker 配置" || docker.cleanup != "Docker 清理" || docker.uninstall != "Docker 卸载" || !docker.configEnabled {
		t.Fatalf("docker labels = %#v", docker)
	}
	podman := containerMenuLabels([]runtime.Runtime{{Name: "podman", Display: "Podman"}})
	if podman.manage != "Podman 管理" || podman.config != "Podman 配置" || podman.cleanup != "Podman 清理" || podman.uninstall != "Podman 卸载" || podman.configEnabled {
		t.Fatalf("podman labels = %#v", podman)
	}
	both := containerMenuLabels([]runtime.Runtime{{Name: "docker", Display: "Docker"}, {Name: "podman", Display: "Podman"}})
	if both.manage != "Docker 管理" || both.cleanup != "Docker 清理" || both.uninstall != "卸载 Docker 或 Podman" || !both.configEnabled {
		t.Fatalf("both labels = %#v", both)
	}
}

func TestContainerRuntimeByChoiceSelectsDockerOrPodman(t *testing.T) {
	installed := []runtime.Runtime{{Name: "docker", Display: "Docker"}, {Name: "podman", Display: "Podman"}}
	docker, ok := containerRuntimeByChoice(installed, "1")
	if !ok || docker.Name != "docker" {
		t.Fatalf("choice 1 = %#v ok=%v", docker, ok)
	}
	podman, ok := containerRuntimeByChoice(installed, "2")
	if !ok || podman.Name != "podman" {
		t.Fatalf("choice 2 = %#v ok=%v", podman, ok)
	}
	if _, ok := containerRuntimeByChoice(installed, "3"); ok {
		t.Fatal("choice 3 was accepted")
	}
}

func TestRuntimeStatusFieldsOmitDockerDetailsWithoutDocker(t *testing.T) {
	fields := runtimeStatusFields(
		[]runtime.Runtime{{Name: "podman", Display: "Podman"}},
		"不应显示",
		"不应显示",
		"不应显示",
	)
	if len(fields) != 1 || fields[0].Label != "当前运行时" || fields[0].Value != "Podman" {
		t.Fatalf("fields = %#v", fields)
	}
}
