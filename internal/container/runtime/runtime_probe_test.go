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
