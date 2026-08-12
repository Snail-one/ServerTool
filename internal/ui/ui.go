package ui

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	dim    = "\033[2m"
	cyan   = "\033[36m"
	blue   = "\033[34m"
	green  = "\033[32m"
	yellow = "\033[33m"
)

type UI struct {
	reader *bufio.Reader
}

func New() *UI {
	return &UI{reader: bufio.NewReader(os.Stdin)}
}

func (u *UI) Ask(prompt string) (string, error) {
	fmt.Print(paint(bold+cyan, "❯ ") + paint(bold, prompt))
	value, err := u.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func (u *UI) Confirm(prompt string) (bool, error) {
	value, err := u.Ask(prompt)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(value, "y"), nil
}

func (u *UI) Pause() {
	u.PauseWithPrompt("按回车返回菜单...")
}

func (u *UI) PauseWithPrompt(prompt string) {
	fmt.Println()
	fmt.Print(paint(dim, prompt))
	_, _ = u.reader.ReadString('\n')
	fmt.Println()
}

func ClearScreen() {
	if !terminalOutput() || strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return
	}
	// \033[2J 清屏, \033[H 光标移到左上角, \033[3J 尝试清除回滚缓冲 (部分终端支持)
	fmt.Print("\033[2J\033[H\033[3J")
}

// MenuTitle prints a consistent, color-aware breadcrumb for terminal menus.
func MenuTitle(parts ...string) {
	clean := make([]string, 0, len(parts)+1)
	clean = append(clean, "ServerTool")
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			clean = append(clean, part)
		}
	}
	title := strings.Join(clean, "  ›  ")
	fmt.Println(paint(cyan, "╭─") + " " + paint(bold+cyan, title))
	fmt.Println(paint(cyan, "╰"+strings.Repeat("─", 46)))
	fmt.Println()
}

// HomeTitle renders the main-menu banner and build version.
func HomeTitle(buildVersion string) {
	buildVersion = strings.TrimSpace(buildVersion)
	if buildVersion == "" {
		buildVersion = "dev"
	}
	fmt.Println(paint(cyan, "╭─") + " " + paint(bold+cyan, "ServerTool"))
	fmt.Println(paint(cyan, "│") + " " + paint(dim, "服务器管理工具") + "  " + Badge("版本 "+buildVersion, true))
	fmt.Println(paint(cyan, "╰"+strings.Repeat("─", 46)))
	fmt.Println()
}

// MenuOption renders a selectable menu row with a visually distinct shortcut.
func MenuOption(key, label string) {
	fmt.Printf("  %s %s %s\n", paint(bold+blue, fmt.Sprintf("%-3s", key)), paint(dim, "│"), label)
}

// MenuExit renders the return/exit row separately from regular actions.
func MenuExit(key, label string) {
	fmt.Printf("  %s %s %s\n", paint(bold+yellow, fmt.Sprintf("%-3s", key)), paint(dim, "└"), paint(dim, label))
}

// Badge returns a compact status label. Positive states use green; states that
// need attention use yellow. It remains plain text when color is unavailable.
func Badge(text string, positive bool) string {
	color := yellow
	if positive {
		color = green
	}
	return paint(color, "["+text+"]")
}

// ConfiguredBadge keeps configuration status wording and color consistent.
func ConfiguredBadge(configured bool) string {
	if configured {
		return Badge("已配置", true)
	}
	return Badge("未配置", false)
}

func paint(style, text string) string {
	if !colorEnabled() {
		return text
	}
	return style + text + reset
}

func colorEnabled() bool {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled || strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return false
	}
	if forced := os.Getenv("CLICOLOR_FORCE"); forced != "" && forced != "0" {
		return true
	}
	return terminalOutput()
}

func terminalOutput() bool {
	info, err := os.Stdout.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
