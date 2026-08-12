package ui

import (
	"bufio"
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

func TestMenuOptionStatusAlignsBadges(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	output := captureStdout(t, func() {
		MenuOptionStatus("1", "容器管理", "[Podman]")
		MenuOptionStatus("4", "系统与用户配置", "[已配置 3/4]")
		MenuOptionStatus("5", "开发环境管理", "[Go go1.26.5]")
	})

	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	wantColumn := -1
	for _, line := range lines {
		badge := strings.Index(line, "[")
		if badge < 0 {
			t.Fatalf("菜单缺少状态徽标：%q", line)
		}
		column := displayWidth(line[:badge])
		if wantColumn < 0 {
			wantColumn = column
		} else if column != wantColumn {
			t.Fatalf("状态徽标未对齐：%q", output)
		}
	}
}

func TestMenuOptionStatusHintKeepsGlobalStatusColumn(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	output := captureStdout(t, func() {
		MenuOptionStatus("1", "SSH 公钥", "[已配置]")
		MenuOptionStatusHint("2", "Vim 配置", "[未配置]", "~/.vimrc")
		MenuOptionStatus("3", "HTTP/HTTPS 代理", "[已配置]")
	})

	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	wantColumn := -1
	for _, line := range lines {
		badge := strings.Index(line, "[")
		if badge < 0 {
			t.Fatalf("菜单缺少状态徽标：%q", line)
		}
		column := displayWidth(line[:badge])
		if wantColumn < 0 {
			wantColumn = column
		} else if column != wantColumn {
			t.Fatalf("全局状态列未对齐：%q", output)
		}
	}
	if !strings.Contains(output, "-- ~/.vimrc") {
		t.Fatalf("状态菜单缺少灰色说明文本：%q", output)
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
	for _, code := range []string{reset, bold, dim, orange, blue, green, yellow, gray} {
		input = strings.ReplaceAll(input, code, "")
	}
	return input
}

func TestNormalizePromptAndConfirmYes(t *testing.T) {
	if got := normalizePrompt("请选择: "); got != "请选择：" {
		t.Fatalf("normalizePrompt() = %q", got)
	}
	view := &UI{reader: bufio.NewReader(strings.NewReader("yes\n"))}
	var confirmed bool
	var err error
	captureStdout(t, func() {
		confirmed, err = view.Confirm("是否继续？(y/N): ")
	})
	if err != nil || !confirmed {
		t.Fatalf("Confirm(yes) = %v, %v", confirmed, err)
	}
}

func TestPrimaryColorAndRecoverableInputStyle(t *testing.T) {
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("NO_COLOR", "")
	os.Unsetenv("NO_COLOR")
	output := captureStdout(t, func() {
		MenuTitle("容器管理")
		InvalidChoice()
	})
	if !strings.Contains(output, orange+"╭─") {
		t.Fatalf("菜单标题未使用橙色主视觉：%q", output)
	}
	if !strings.Contains(output, yellow+"无效选项，请重新输入"+reset) {
		t.Fatalf("无效输入未使用统一黄色提示：%q", output)
	}
}

func TestHomeSubtitleUsesPrimaryColor(t *testing.T) {
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("NO_COLOR", "")
	os.Unsetenv("NO_COLOR")
	output := captureStdout(t, func() {
		HomeTitle("v1.2.3")
	})
	if !strings.Contains(output, orange+"服务器管理工具"+reset) {
		t.Fatalf("首页副标题未使用橙色主视觉：%q", output)
	}
}

func TestReportPaletteUsesSemanticColors(t *testing.T) {
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("NO_COLOR", "")
	os.Unsetenv("NO_COLOR")

	if got := PrimaryBoldText("标题"); got != bold+orange+"标题"+reset {
		t.Fatalf("PrimaryBoldText() = %q", got)
	}
	if got := InfoText("字段"); got != blue+"字段"+reset {
		t.Fatalf("InfoText() = %q", got)
	}
	if got := MutedText("配置项"); got != gray+"配置项"+reset {
		t.Fatalf("MutedText() = %q", got)
	}

	want := map[string]string{
		"安全": green,
		"风险": red,
		"信息": blue,
		"未知": yellow,
	}
	for status, color := range want {
		if got := StatusBadge(status); got != color+"["+status+"]"+reset {
			t.Fatalf("StatusBadge(%q) = %q", status, got)
		}
	}
}

func TestPauseNormalizesEllipsis(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	view := &UI{reader: bufio.NewReader(strings.NewReader("\n"))}
	output := captureStdout(t, func() {
		view.PauseWithPrompt("按回车继续...")
	})
	if !strings.Contains(output, "按回车继续… ") || strings.Contains(output, "...") {
		t.Fatalf("暂停提示未统一中文省略号：%q", output)
	}
}
