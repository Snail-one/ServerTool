package runtime

import (
	"errors"
	"strings"
	"testing"
)

func TestProbeContainerRuntimeScenarios(t *testing.T) {
	tests := []struct {
		name       string
		docker     bool
		podman     bool
		version    string
		infoErr    error
		wantNames  []string
		wantDetail string
	}{
		{name: "docker engine", docker: true, version: "Docker version 29", wantNames: []string{"docker"}},
		{name: "podman compatibility", docker: true, version: "podman version 5", wantNames: []string{"podman"}},
		{name: "daemon abnormal", docker: true, version: "Docker version 29", infoErr: errors.New("daemon down"), wantNames: []string{"docker"}, wantDetail: "服务异常"},
		{name: "both", docker: true, podman: true, version: "Docker version 29", wantNames: []string{"docker", "podman"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := probeContainerRuntimes(func(name string) bool {
				return name == "docker" && tt.docker || name == "podman" && tt.podman
			}, func(_ string, args ...string) (string, error) {
				if args[0] == "--version" {
					return tt.version, nil
				}
				if tt.infoErr != nil {
					return "Cannot connect to daemon", tt.infoErr
				}
				return "Server Version: 29", nil
			})
			if len(got) != len(tt.wantNames) {
				t.Fatalf("runtimes = %v, want names %v", got, tt.wantNames)
			}
			for i, name := range tt.wantNames {
				if got[i].Name != name {
					t.Fatalf("runtime[%d] = %v, want %s", i, got[i], name)
				}
			}
			if tt.wantDetail != "" && !strings.Contains(got[0].Display, tt.wantDetail) {
				t.Fatalf("display = %q, want %q", got[0].Display, tt.wantDetail)
			}
		})
	}
}

func TestProbeReportsDockerCommandBeforeRunningIt(t *testing.T) {
	currentStep := ""
	got := probeContainerRuntimesWithProgress(func(name string) bool {
		return name == "docker"
	}, func(_ string, args ...string) (string, error) {
		switch args[0] {
		case "--version":
			if currentStep != "Docker 版本（docker --version）" {
				t.Fatalf("version command started with step %q", currentStep)
			}
			return "Docker version 29", nil
		case "info":
			if currentStep != "Docker 服务（docker info）" {
				t.Fatalf("daemon command started with step %q", currentStep)
			}
			return "Cannot connect to daemon", errors.New("daemon down")
		default:
			t.Fatalf("unexpected Docker arguments: %v", args)
			return "", nil
		}
	}, func(label string, detect func()) {
		currentStep = label
		detect()
		currentStep = ""
	})
	if len(got) != 1 || got[0].Name != "docker" || !strings.Contains(got[0].Display, "服务异常：Cannot connect to daemon") {
		t.Fatalf("progress changed daemon failure detection: %v", got)
	}
}
