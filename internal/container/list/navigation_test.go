package list

import (
	"errors"
	"os"
	"testing"

	"snail_tool/internal/container/update"
	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

func TestProjectCancellationReturnsWithoutAnExtraPause(t *testing.T) {
	for _, answer := range []string{"0", "q", "返回", "n", ""} {
		t.Run("answer="+answer, func(t *testing.T) {
			file, err := os.CreateTemp(t.TempDir(), "input-*")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = file.Close() })
			// Cancel a down confirmation, leave the project page, then read the
			// next parent-menu input. No Compose command may be executed.
			if _, err := file.WriteString("4\n" + answer + "\nq\nnext\n"); err != nil {
				t.Fatal(err)
			}
			if _, err := file.Seek(0, 0); err != nil {
				t.Fatal(err)
			}
			previous := os.Stdin
			os.Stdin = file
			view := ui.New()
			os.Stdin = previous
			compose := update.ComposeCommand{Display: "must-not-execute"}
			if err := manageSingleProject(view, compose, t.TempDir()); !errors.Is(err, shared.ErrReturnToMenu) {
				t.Fatalf("cancelled project page did not return: %v", err)
			}
			if next, err := view.Ask("下一项："); err != nil || next != "next" {
				t.Fatalf("project cancellation consumed parent input: next=%q err=%v", next, err)
			}
		})
	}
}
