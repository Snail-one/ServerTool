package runtime

import (
	"testing"

	"snail_tool/internal/shared"
)

func TestSelectRuntimeToUninstall(t *testing.T) {
	runtimes := []Runtime{{Name: "docker", Display: "Docker"}, {Name: "podman", Display: "Podman"}}
	for _, tt := range []struct {
		answer  string
		name    string
		proceed bool
	}{
		{answer: "1", name: "docker", proceed: true},
		{answer: "2", name: "podman", proceed: true},
		{answer: "q"},
	} {
		prompt := &fakeDockerUninstallPrompt{answers: []string{tt.answer}}
		selected, proceed, err := selectRuntimeToUninstall(prompt, runtimes)
		if err != nil {
			t.Fatal(err)
		}
		if proceed != tt.proceed || selected.Name != tt.name {
			t.Fatalf("answer=%q selected=%q proceed=%v", tt.answer, selected.Name, proceed)
		}
	}
}

func TestUninstallBackReturnsThroughModeAndRuntimeSelection(t *testing.T) {
	runtimes := []Runtime{{Name: "docker", Display: "Docker"}, {Name: "podman", Display: "Podman"}}
	prompt := &fakeDockerUninstallPrompt{answers: []string{"1", "2", "q", "2", "1", "q", "q", "parent-next"}}
	var confirmations []string
	removed, err := uninstallDetectedRuntimes(prompt, runtimes, func(rt Runtime) (bool, error) {
		choose := selectDockerUninstallMode
		if rt.Name == "podman" {
			choose = selectPodmanUninstallMode
		}
		return runUninstallMode(prompt, choose, func(removeData bool) (bool, error) {
			confirmations = append(confirmations, rt.Name)
			return false, shared.ErrReturnToMenu
		})
	})
	if err != nil || removed {
		t.Fatalf("cancelled uninstall: removed=%v err=%v", removed, err)
	}
	if len(confirmations) != 2 || confirmations[0] != "docker" || confirmations[1] != "podman" {
		t.Fatalf("did not revisit the runtime selector: %v", confirmations)
	}
	if next, err := prompt.Ask("parent"); err != nil || next != "parent-next" {
		t.Fatalf("uninstall skipped a page: next=%q err=%v", next, err)
	}
}
