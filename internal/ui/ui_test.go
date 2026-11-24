package ui

import (
	"bufio"
	"io"
	"os"
	"strings"
	"testing"
)

func TestMenuRowsAlignAndHintUsesGray(t *testing.T) {
	t.Setenv("TERM", "xterm")
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
	lines := strings.Split(strings.TrimSuffix(plain, "\n\n"), "\n")
	if len(lines) != 4 || lines[2] != "" || strings.Index(lines[0], "管理") != strings.Index(lines[3], "返回") {
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
		MenuOptionStatus("4", "Bash 自动补全", "[已安装]")
		MenuOptionStatus("2", "Go 语言环境", "[已安装 go1.26.5]")
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
	for _, code := range []string{reset, bold, dim, orange, blue, green, yellow, red, gray, lightBlue, white} {
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

func TestAskMenuChoiceDoesNotRepeatBufferedInvalidChoices(t *testing.T) {
	t.Setenv("TERM", "dumb")
	for _, answer := range []string{"2", "q"} {
		t.Run(answer, func(t *testing.T) {
			input := strings.Repeat("invalid\n\n", 20) + answer + "\nnext\n"
			view := &UI{reader: bufio.NewReader(strings.NewReader(input))}
			var choice string
			var err error
			output := captureStdout(t, func() {
				choice, err = AskMenuChoice(view, "请选择操作", "", map[string]string{"2": "remove"}, func() { MenuOption("2", "删除") })
			})
			if strings.Count(output, "无效选项") != 1 || strings.Count(output, "请选择操作") != 1 {
				t.Fatalf("retry output repeated: %q", output)
			}
			if answer == "q" && err != ErrReturnToMenu || answer == "2" && (err != nil || choice != "remove") {
				t.Fatalf("choice=%q, err=%v", choice, err)
			}
			if next, err := view.ReadLine(); err != nil || next != "next" {
				t.Fatalf("next input=%q, err=%v", next, err)
			}
		})
	}
}

func TestPrimaryColorAndRecoverableInputStyle(t *testing.T) {
	t.Setenv("TERM", "xterm")
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
	t.Setenv("TERM", "xterm")
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
	t.Setenv("TERM", "xterm")
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("NO_COLOR", "")
	os.Unsetenv("NO_COLOR")

	if got := PrimaryBoldText("标题"); got != bold+orange+"标题"+reset {
		t.Fatalf("PrimaryBoldText() = %q", got)
	}
	if got := InfoText("字段"); got != orange+"字段"+reset {
		t.Fatalf("InfoText() = %q", got)
	}
	if got := SuccessBoldText("完成"); got != bold+green+"完成"+reset {
		t.Fatalf("SuccessBoldText() = %q", got)
	}
	if got := MutedText("配置项"); got != gray+"配置项"+reset {
		t.Fatalf("MutedText() = %q", got)
	}

	want := map[string]string{
		"安全": green,
		"风险": red,
		"信息": orange,
		"未知": yellow,
	}
	for status, color := range want {
		if got := StatusBadge(status); got != color+"["+status+"]"+reset {
			t.Fatalf("StatusBadge(%q) = %q", status, got)
		}
	}
}

func TestInstallationBadgeUsesOrangeAndGray(t *testing.T) {
	t.Setenv("TERM", "xterm")
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("NO_COLOR", "")
	os.Unsetenv("NO_COLOR")

	if got := InstallationBadge(true); got != orange+"[已安装]"+reset {
		t.Fatalf("InstallationBadge(true) = %q", got)
	}
	if got := InstallationBadge(false); got != gray+"[未安装]"+reset {
		t.Fatalf("InstallationBadge(false) = %q", got)
	}
	if got := SoftwareBadge("Docker 28", true); got != orange+"[Docker 28]"+reset {
		t.Fatalf("SoftwareBadge(installed) = %q", got)
	}
	if got := SoftwareBadge("未安装", false); got != gray+"[未安装]"+reset {
		t.Fatalf("SoftwareBadge(missing) = %q", got)
	}
	if got := CurrentBadge(true, true); got != orange+"[当前]"+reset {
		t.Fatalf("CurrentBadge(enabled) = %q", got)
	}
	if got := CurrentBadge(true, false); got != gray+"[当前]"+reset {
		t.Fatalf("CurrentBadge(disabled) = %q", got)
	}
	if got := CurrentBadge(false, true); got != "" {
		t.Fatalf("CurrentBadge(unselected) = %q", got)
	}
	if got := ConfiguredBadge(true); got != orange+"[已配置]"+reset {
		t.Fatalf("ConfiguredBadge(true) = %q", got)
	}
	if got := ConfiguredBadge(false); got != gray+"[未配置]"+reset {
		t.Fatalf("ConfiguredBadge(false) = %q", got)
	}
	if got := ConfigurationBadge("已配置 3/4", true); got != orange+"[已配置 3/4]"+reset {
		t.Fatalf("ConfigurationBadge(partial) = %q", got)
	}
}

func TestSemanticCardsUseSharedLayoutAndColors(t *testing.T) {
	t.Setenv("TERM", "xterm")
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("NO_COLOR", "")
	os.Unsetenv("NO_COLOR")

	output := captureStdout(t, func() {
		PrintInfoCard("运行状态", CardField{Label: "nftables", Value: InstallationBadge(true) + " v1.1.3"})
		PrintSectionCard("当前配置")
		PrintSuccessCard("配置完成",
			CardField{Label: "端口", Value: "30948"},
			CardField{Label: "SSH 配置", Value: "/usr/sbin/sshd -T", Detail: "实际生效配置"},
		)
		PrintWarningCard("确认变更")
		PrintDangerCard("永久删除", CardField{Label: "路径", Value: "/var/lib/docker"})
	})
	for _, header := range []string{
		bold + orange + "╭─ 运行状态" + reset,
		bold + orange + "╭─ 当前配置" + reset,
		bold + green + "╭─ 配置完成" + reset,
		bold + yellow + "╭─ 确认变更" + reset,
		bold + red + "╭─ 永久删除" + reset,
	} {
		if !strings.Contains(output, header+"\n") {
			t.Fatalf("卡片顶栏缺少 %q：%q", header, output)
		}
	}
	if !strings.Contains(output, orange+"端口："+reset+white+"30948"+reset) {
		t.Fatalf("卡片字段未使用橙色标签和白色值：%q", output)
	}
	if !strings.Contains(output, orange+"nftables："+reset+white+"[已安装] v1.1.3"+reset) {
		t.Fatalf("卡片徽标和后续文字未统一为白色：%q", output)
	}
	if !strings.Contains(output, gray+"实际生效配置"+reset) {
		t.Fatalf("卡片详情未使用灰色：%q", output)
	}
}

func TestCardStatusColors(t *testing.T) {
	t.Setenv("TERM", "xterm")
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("NO_COLOR", "")
	os.Unsetenv("NO_COLOR")
	for _, test := range []struct {
		value   string
		changed bool
		want    string
	}{
		{value: "已开启", want: white + "已开启" + reset},
		{value: "开启（默认）", want: white + "开启（默认）" + reset},
		{value: "IPv4 已开启 · IPv6 已关闭", want: white + "IPv4 已开启 ·" + gray + " IPv6 已关闭" + white + reset},
		{value: "已拦截", want: white + gray + "已拦截" + white + reset},
		{value: "已关闭（默认）", want: white + gray + "已关闭（默认）" + white + reset},
		{value: "[未安装]", want: white + gray + "[未安装]" + white + reset},
		{value: "未生效 · 软件包未安装", want: white + gray + "未生效 " + white + "·" + gray + " 软件包未安装" + white + reset},
		{value: "未生效 · 索引刷新策略未开启", want: white + gray + "未生效 " + white + "·" + gray + " 索引刷新策略未开启" + white + reset},
		{value: "未找到", want: white + gray + "未找到" + white + reset},
		{value: "已屏蔽", want: white + gray + "已屏蔽" + white + reset},
		{value: "无法判断", want: white + yellow + "无法判断" + white + reset},
		{value: "未知间隔", want: white + yellow + "未知间隔" + white + reset},
		{value: "规则未同步", want: white + yellow + "规则未同步" + white + reset},
		{value: "未同步 · 配置与内核规则不同", want: white + yellow + "未同步 " + white + "· 配置与内核规则不同" + reset},
		{value: "后端不兼容（nftables）", want: white + yellow + "后端不兼容（nftables）" + white + reset},
		{value: "等待 Docker 转发链", want: white + yellow + "等待 Docker 转发链" + white + reset},
		{value: "当前系统不支持 APT 更新管理", want: white + yellow + "当前系统不支持 APT 更新管理" + white + reset},
		{value: "无法确认 · 定时器检测失败", want: white + yellow + "无法确认 " + white + "·" + red + " 定时器检测失败" + white + reset},
		{value: "检测失败", want: white + red + "检测失败" + white + reset},
		{value: "APT 策略检测失败", want: white + red + "APT 策略检测失败" + white + reset},
		{value: "运行失败", want: white + red + "运行失败" + white + reset},
		{value: "调度已生效 · 每 7 天", want: white + "调度已生效 · 每 7 天" + reset},
		{value: "已关闭", changed: true, want: lightBlue + "已关闭" + reset},
		{value: "默认丢弃（drop）", want: white + "默认丢弃（drop）" + reset},
		{value: "关闭会影响 Docker、VPN 和路由转发；开启后仍受转发规则约束", want: white + "关闭会影响 Docker、VPN 和路由转发；开启后仍受转发规则约束" + reset},
		{value: "未找到 inet snail；不能据此判断服务器端口全部开放或关闭", want: white + "未找到 inet snail；不能据此判断服务器端口全部开放或关闭" + reset},
	} {
		if got := cardValue(test.value, test.changed); got != test.want {
			t.Errorf("cardValue(%q, %v) = %q, want %q", test.value, test.changed, got, test.want)
		}
	}
	t.Setenv("NO_COLOR", "1")
	if got := cardValue("IPv4 已开启 · IPv6 已关闭", true); got != "IPv4 已开启 · IPv6 已关闭" {
		t.Fatalf("cardValue with NO_COLOR = %q", got)
	}
}

func TestTableCellPadsColoredTextByVisibleWidth(t *testing.T) {
	t.Setenv("TERM", "xterm")
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("NO_COLOR", "")
	os.Unsetenv("NO_COLOR")

	cell := TableCell(SuccessText("运行中"), 8)
	if got := displayWidth(cell); got != 8 {
		t.Fatalf("TableCell visible width = %d, want 8: %q", got, cell)
	}
	if !strings.Contains(cell, green+"运行中"+reset) {
		t.Fatalf("TableCell did not preserve color: %q", cell)
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
