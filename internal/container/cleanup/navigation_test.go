package cleanup

import (
	"os"
	"snail_tool/internal/container/runtime"
	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
	"testing"
)

func TestCleanupBackReturnsToRuntimeSelection(t *testing.T) {
	view := navigationUI(t, "1\n2\nq\nparent-next\n")
	runtimes := []runtime.Runtime{{Name: "docker", Display: "Docker"}, {Name: "podman", Display: "Podman"}}
	var visited []string
	err := cleanupDetectedRuntimes(view, runtimes, func(rt runtime.Runtime) error { visited = append(visited, rt.Name); return shared.ErrReturnToMenu })
	if err != shared.ErrReturnToMenu {
		t.Fatal(err)
	}
	if len(visited) != 2 || visited[0] != "docker" || visited[1] != "podman" {
		t.Fatalf("did not revisit selector: %v", visited)
	}
	if next, err := view.Ask("parent"); err != nil || next != "parent-next" {
		t.Fatalf("cleanup skipped selector: next=%q err=%v", next, err)
	}
}

func navigationUI(t *testing.T, input string) *ui.UI {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "input")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	if _, err := f.WriteString(input); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	previous := os.Stdin
	os.Stdin = f
	view := ui.New()
	os.Stdin = previous
	return view
}
