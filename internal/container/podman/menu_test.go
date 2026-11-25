package podman

import "testing"

func TestStatusFieldsOmitDockerDetails(t *testing.T) {
	fields := statusFields("Podman")
	if len(fields) != 1 || fields[0].Label != "当前运行时" || fields[0].Value != "Podman" {
		t.Fatalf("fields = %#v", fields)
	}
}

func TestUninstallLabel(t *testing.T) {
	if got := uninstallLabel("Podman"); got != "Podman 卸载" {
		t.Fatalf("podman label = %q", got)
	}
}
