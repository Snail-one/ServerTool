package runtime

import "testing"

func TestRuntimeForCommands(t *testing.T) {
	tests := []struct {
		name      string
		docker    bool
		podman    bool
		wantName  string
		wantFound bool
	}{
		{name: "docker first", docker: true, podman: true, wantName: "docker", wantFound: true},
		{name: "podman only", podman: true, wantName: "podman", wantFound: true},
		{name: "missing", wantFound: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := runtimeForCommands(tt.docker, tt.podman)
			if ok != tt.wantFound {
				t.Fatalf("found = %v, want %v", ok, tt.wantFound)
			}
			if got.Name != tt.wantName {
				t.Fatalf("runtime name = %q, want %q", got.Name, tt.wantName)
			}
		})
	}
}
