package ssh

import (
	"errors"
	"os"
	"strings"
	"testing"

	"snail_tool/internal/ui"
)

func TestSSHPagesReturnToManagementWithoutExtraPause(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	input, err := os.CreateTemp(t.TempDir(), "ssh-input")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if _, err := input.WriteString("2\nq\n3\nr\nq\n4\nq\nq\nparent-next\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := input.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	previousInput := os.Stdin
	os.Stdin = input
	view := ui.New()
	os.Stdin = previousInput

	output, err := os.CreateTemp(t.TempDir(), "ssh-output")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	previousOutput := os.Stdout
	os.Stdout = output
	err = Run(view)
	os.Stdout = previousOutput
	if !errors.Is(err, ui.ErrReturnToMenu) {
		t.Fatalf("SSH management did not return: %v", err)
	}
	if next, err := view.Ask("上一级"); err != nil || next != "parent-next" {
		t.Fatalf("SSH page consumed parent input: next=%q err=%v", next, err)
	}

	text, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), "设置 SSH 端口和认证策略\n\n  r   刷新状态") {
		t.Error("numeric actions and letter shortcuts should have a blank line between them")
	}
	for title, want := range map[string]int{
		"SSH 管理  ›  SSH 安全策略":      1,
		"SSH 管理  ›  查看生效配置":        2,
		"SSH 管理  ›  Root SSH 登录策略": 1,
	} {
		if got := strings.Count(string(text), title); got != want {
			t.Errorf("page %q displayed %d times, want %d", title, got, want)
		}
	}
}
