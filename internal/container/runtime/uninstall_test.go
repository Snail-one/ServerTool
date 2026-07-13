package runtime

import "testing"

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
