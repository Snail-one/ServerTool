package container

import (
	"strings"
	"testing"
)

func TestEnsureContainerRuntimeAbsent(t *testing.T) {
	tests := []struct {
		name       string
		docker     bool
		podman     bool
		wantError  bool
		wantDetail string
	}{
		{name: "none installed"},
		{name: "docker installed", docker: true, wantError: true, wantDetail: "Docker"},
		{name: "podman installed", podman: true, wantError: true, wantDetail: "Podman"},
		{name: "both installed", docker: true, podman: true, wantError: true, wantDetail: "Docker、Podman"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ensureContainerRuntimeAbsent(func(name string) bool {
				switch name {
				case "docker":
					return tt.docker
				case "podman":
					return tt.podman
				default:
					return false
				}
			}, func(string, ...string) (string, error) { return "Docker version test", nil })
			if (err != nil) != tt.wantError {
				t.Fatalf("error = %v, wantError %v", err, tt.wantError)
			}
			if err != nil && !strings.Contains(err.Error(), tt.wantDetail) {
				t.Fatalf("error = %q, want detail %q", err, tt.wantDetail)
			}
		})
	}
}
