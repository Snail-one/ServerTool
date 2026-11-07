package list

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"snail_tool/internal/container/runtime"
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

func TestComposeScanReturnsThroughProjectListAndDirectoryPrompt(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\nprintf '[]\\n'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	root := t.TempDir()
	project := filepath.Join(root, "app")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "compose.yml"), []byte("services: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, sequence := range []string{
		"1\nq\nq\nq\n",      // actions -> project list -> scan directories -> Compose
		"1\n2\ny\n\nq\nq\n", // completed action -> project list -> scan directories
	} {
		t.Run(sequence, func(t *testing.T) {
			view := navigationUI(t, root+"\n"+sequence+"parent-next\n")
			if err := manageComposeProjects(view, runtime.Runtime{Name: "docker"}, false); !errors.Is(err, shared.ErrReturnToMenu) {
				t.Fatal(err)
			}
			if next, err := view.Ask("parent"); err != nil || next != "parent-next" {
				t.Fatalf("Compose skipped a page: next=%q err=%v", next, err)
			}
		})
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
