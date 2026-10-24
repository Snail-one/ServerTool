package cleanup

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/container/runtime"
	"snail_tool/internal/ui"
)

func TestContainerDiskUsageFieldsPreserveAllMetrics(t *testing.T) {
	for _, name := range []string{"docker", "podman"} {
		t.Run(name, func(t *testing.T) {
			got, err := containerDiskUsageFields(runtime.Runtime{Name: name, Display: name}, func(command string, args ...string) (string, error) {
				total := "{{.TotalCount}}"
				if name == "podman" {
					total = "{{.Total}}"
				}
				wantArgs := []string{"system", "df", "--format", "{{.Type}}\t" + total + "\t{{.Active}}\t{{.Size}}\t{{.Reclaimable}}"}
				if command != name || !reflect.DeepEqual(args, wantArgs) {
					t.Fatalf("unexpected command: %s %v", command, args)
				}
				return "Images\t3\t0\t713.6MB\t596.1MB (83%)\nContainers\t0\t0\t0B\t0B\nLocal Volumes\t2\t0\t84.8MB\t84.8MB (100%)\nBuild Cache\t44\t0\t1.126 GB\t507.8 MB\r\n", nil
			})
			want := []ui.CardField{
				{Label: "镜像", Value: "占用 713.6MB · 可回收 596.1MB (83%)", Detail: "总数 3 · 活跃 0"},
				{Label: "容器", Value: "占用 0B · 可回收 0B", Detail: "总数 0 · 活跃 0"},
				{Label: "数据卷", Value: "占用 84.8MB · 可回收 84.8MB (100%)", Detail: "总数 2 · 活跃 0"},
				{Label: "构建缓存", Value: "占用 1.126 GB · 可回收 507.8 MB", Detail: "总数 44 · 活跃 0"},
			}
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("fields = %#v, %v; want %#v", got, err, want)
			}
		})
	}
}

func TestContainerDiskUsageFieldsRejectIncompleteResults(t *testing.T) {
	for _, text := range []string{"TYPE TOTAL ACTIVE SIZE RECLAIMABLE", "Images\t3\t0\t100MB", "Images\tbad\t0\t100MB\t50MB", "Images\t3\t-1\t100MB\t50MB", "Images\t3\t0\t\t50MB"} {
		if fields, err := containerDiskUsageFields(runtime.Runtime{Name: "docker"}, func(string, ...string) (string, error) { return text, nil }); err == nil || len(fields) != 0 {
			t.Fatalf("incomplete results accepted: %q => %#v, %v", text, fields, err)
		}
	}
	failure := errors.New("exit status 1")
	if fields, err := containerDiskUsageFields(runtime.Runtime{Name: "docker", Display: "Docker"}, func(string, ...string) (string, error) {
		return "Cannot connect to the Docker daemon\n", failure
	}); !errors.Is(err, failure) || !strings.Contains(err.Error(), "Cannot connect") || len(fields) != 0 {
		t.Fatalf("command failure not preserved: %#v, %v", fields, err)
	}
}

func TestContainerDiskUsageFieldsHandleEmptyAndNewResourceTypes(t *testing.T) {
	if fields, err := containerDiskUsageFields(runtime.Runtime{Name: "docker"}, func(string, ...string) (string, error) { return "\n", nil }); err != nil || len(fields) != 0 {
		t.Fatalf("empty result: %#v, %v", fields, err)
	}
	fields, err := containerDiskUsageFields(runtime.Runtime{Name: "docker"}, func(string, ...string) (string, error) { return "Other Resources\t1\t1\t10MB\t0B\n", nil })
	if err != nil || len(fields) != 1 || fields[0].Label != "Other Resources" {
		t.Fatalf("new resource type was lost: %#v, %v", fields, err)
	}
}
