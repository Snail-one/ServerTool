package ui

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestMenuRowsAlignAndHintUsesGray(t *testing.T) {
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("NO_COLOR", "")
	os.Unsetenv("NO_COLOR")

	output := captureStdout(t, func() {
		MenuOptionHint("1", "管理运行中的 Compose 项目", "docker compose ls")
		MenuOptionHint("2", "更新运行中的 Compose 应用", "docker compose pull")
		MenuExit("0/q", "返回")
	})

	if !strings.Contains(output, gray+"-- docker compose ls"+reset) {
		t.Fatalf("菜单说明未使用灰色：%q", output)
	}
	plain := stripANSI(output)
	lines := strings.Split(strings.TrimSuffix(plain, "\n"), "\n")
	if len(lines) != 3 || strings.Index(lines[0], "管理") != strings.Index(lines[2], "返回") {
		t.Fatalf("菜单文字未对齐：%q", plain)
	}
	firstHint := strings.Index(lines[0], "--")
	secondHint := strings.Index(lines[1], "--")
	if displayWidth(lines[0][:firstHint]) != displayWidth(lines[1][:secondHint]) {
		t.Fatalf("菜单说明未对齐：%q", plain)
	}
}

func captureStdout(t *testing.T, run func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = writer
	t.Cleanup(func() { os.Stdout = previous })
	run()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func stripANSI(input string) string {
	for _, code := range []string{reset, bold, dim, cyan, blue, green, yellow, gray} {
		input = strings.ReplaceAll(input, code, "")
	}
	return input
}
