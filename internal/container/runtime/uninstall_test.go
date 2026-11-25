package runtime

import (
	"errors"
	"testing"

	"snail_tool/internal/ui"
)

type fakeRuntimePrompt struct {
	answers []string
}

func (prompt *fakeRuntimePrompt) Ask(string) (string, error) {
	if len(prompt.answers) == 0 {
		return "", errors.New("no answer")
	}
	answer := prompt.answers[0]
	prompt.answers = prompt.answers[1:]
	return answer, nil
}

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
		prompt := &fakeRuntimePrompt{answers: []string{tt.answer}}
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
	prompt := &fakeRuntimePrompt{answers: []string{"1", "2", "q", "parent-next"}}
	var confirmations []string
	removed, err := uninstallDetectedRuntimes(prompt, runtimes, func(rt Runtime) (bool, error) {
		confirmations = append(confirmations, rt.Name)
		return false, ui.ErrReturnToMenu
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
