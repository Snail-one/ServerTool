package app

import (
	"io"
	"os"
	"strings"
	"testing"

	"snail_tool/internal/status"
	"snail_tool/internal/version"
)

func TestShowMenuIncludesVersionAndStatus(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	previousVersion := version.Version
	version.Version = "v9.8.7"
	t.Cleanup(func() { version.Version = previousVersion })

	output := captureStdout(t, func() {
		showMenu(status.Status{
			Runtime:     "未安装",
			GoVersion:   "go1.25.1",
			Configured:  2,
			ConfigTotal: 3,
			UPS:         true,
		})
	})

	for _, expected := range []string{
		"ServerTool",
		"[版本 v9.8.7]",
		"容器管理",
		"[未安装]",
		"开发环境",
		"[Go go1.25.1]",
		"清理配置",
		"系统工具",
		"[UPS 已配置]",
		"0/q 退出",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("菜单输出缺少 %q：\n%s", expected, output)
		}
	}
	if strings.Contains(output, "\033[") {
		t.Fatalf("NO_COLOR 模式不应包含 ANSI 转义序列：%q", output)
	}
	badgeColumn := -1
	for _, line := range strings.Split(output, "\n") {
		if !isMainStatusRow(line) {
			continue
		}
		badge := strings.Index(line, "[")
		if badge < 0 {
			t.Fatalf("主菜单状态徽标缺失：%q", line)
		}
		column := testDisplayWidth(line[:badge])
		if badgeColumn < 0 {
			badgeColumn = column
		} else if column != badgeColumn {
			t.Fatalf("主菜单状态徽标未对齐：\n%s", output)
		}
	}
}

func isMainStatusRow(line string) bool {
	for _, label := range []string{"1   容器管理", "2   一键配置", "3   SSH 管理", "4   系统与用户配置", "5   开发环境", "7   系统工具"} {
		if strings.Contains(line, label) {
			return true
		}
	}
	return false
}

func testDisplayWidth(text string) int {
	width := 0
	for _, current := range text {
		if current >= 0x2e80 {
			width += 2
		} else {
			width++
		}
	}
	return width
}

func captureStdout(t *testing.T, run func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = writer
	defer func() { os.Stdout = previous }()
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
