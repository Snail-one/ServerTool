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
	orange = "\033[38;5;208m"
	blue   = "\033[34m"
	green  = "\033[32m"
	yellow = "\033[33m"
	red    = "\033[31m"
	gray   = "\033[90m"
)

type UI struct {
	reader *bufio.Reader
}

// CardField is one labeled value in a terminal information card. Detail is
// rendered on a muted continuation line and is useful for paths or directives.
type CardField struct {
	Label  string
	Value  string
	Detail string
}

func New() *UI {
	return &UI{reader: bufio.NewReader(os.Stdin)}
}

func (u *UI) Ask(prompt string) (string, error) {
	prompt = normalizePrompt(prompt)
	fmt.Print(paint(bold+orange, "❯ ") + paint(bold, prompt) + " ")
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
	value = strings.TrimSpace(value)
	return strings.EqualFold(value, "y") || strings.EqualFold(value, "yes"), nil
}

func (u *UI) Pause() {
	u.PauseWithPrompt("按回车返回菜单…")
}

func (u *UI) PauseWithPrompt(prompt string) {
	prompt = strings.TrimSpace(prompt)
	if strings.HasSuffix(prompt, "...") {
		prompt = strings.TrimSuffix(prompt, "...") + "…"
	}
	fmt.Println()
	fmt.Print(paint(dim, prompt) + " ")
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
	fmt.Println(paint(orange, "╭─") + " " + paint(bold+orange, title))
	fmt.Println(paint(orange, "╰"+strings.Repeat("─", 46)))
	fmt.Println()
}

// HomeTitle renders the main-menu banner and build version.
func HomeTitle(buildVersion string) {
	buildVersion = strings.TrimSpace(buildVersion)
	if buildVersion == "" {
		buildVersion = "dev"
	}
	fmt.Println(paint(orange, "╭─") + " " + paint(bold+orange, "ServerTool"))
	fmt.Println(paint(orange, "│") + " " + paint(orange, "服务器管理工具") + "  " + Badge("版本 "+buildVersion, true))
	fmt.Println(paint(orange, "╰"+strings.Repeat("─", 46)))
	fmt.Println()
}

// MenuOption renders a selectable menu row with a visually distinct shortcut.
func MenuOption(key, label string) {
	fmt.Printf("  %s %s\n", menuKey(key, bold+blue), label)
}

// MenuOptionHint renders secondary command or impact text in gray so action
// names remain easy to scan. The separator is part of the secondary text.
func MenuOptionHint(key, label, hint string) {
	hint = strings.TrimSpace(hint)
	if hint == "" {
		MenuOption(key, label)
		return
	}
	fmt.Printf("  %s %s%s\n", menuKey(key, bold+blue), padDisplay(label, 22), paint(gray, "-- "+hint))
}

// MenuOptionStatus keeps status badges in a fixed column across menu rows.
func MenuOptionStatus(key, label, status string) {
	status = strings.TrimSpace(status)
	if status == "" {
		MenuOption(key, label)
		return
	}
	fmt.Printf("  %s %s%s\n", menuKey(key, bold+blue), padDisplay(label, 18), status)
}

// MenuOptionStatusHint combines the global status column with a gray detail
// column. It is useful when an item has both state and a managed path.
func MenuOptionStatusHint(key, label, status, hint string) {
	status = strings.TrimSpace(status)
	hint = strings.TrimSpace(hint)
	if hint == "" {
		MenuOptionStatus(key, label, status)
		return
	}
	fmt.Printf(
		"  %s %s%s%s\n",
		menuKey(key, bold+blue),
		padDisplay(label, 18),
		padDisplay(status, 16),
		paint(gray, "-- "+hint),
	)
}

// MenuExit renders the return/exit row separately from regular actions.
func MenuExit(key, label string) {
	fmt.Printf("  %s %s\n", menuKey(key, bold+yellow), paint(dim, label))
}

// MenuSection renders a consistent heading for choices nested inside a screen.
func MenuSection(label string) {
	fmt.Println(paint(bold, strings.TrimSpace(label)))
}

// InvalidChoice keeps recoverable input errors consistent across menus.
func InvalidChoice() {
	fmt.Println(paint(yellow, "无效选项，请重新输入"))
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

// PrimaryText, InfoText and MutedText expose the shared terminal palette for
// non-menu reports.
func PrimaryText(text string) string {
	return paint(orange, text)
}

func PrimaryBoldText(text string) string {
	return paint(bold+orange, text)
}

func InfoText(text string) string {
	return paint(blue, text)
}

func SuccessBoldText(text string) string {
	return paint(bold+green, text)
}

func WarningBoldText(text string) string {
	return paint(bold+yellow, text)
}

func DangerBoldText(text string) string {
	return paint(bold+red, text)
}

func SuccessText(text string) string {
	return paint(green, text)
}

func WarningText(text string) string {
	return paint(yellow, text)
}

func DangerText(text string) string {
	return paint(red, text)
}

func MutedText(text string) string {
	return paint(gray, text)
}

// PrintInfoCard and the semantic variants keep results and key information in
// one consistent ServerTool card. Values may already contain colored badges.
func PrintInfoCard(title string, fields ...CardField) {
	printCard(PrimaryBoldText(strings.TrimSpace(title)), fields)
}

func PrintSuccessCard(title string, fields ...CardField) {
	printCard(SuccessBoldText(strings.TrimSpace(title)), fields)
}

func PrintWarningCard(title string, fields ...CardField) {
	printCard(WarningBoldText(strings.TrimSpace(title)), fields)
}

func PrintDangerCard(title string, fields ...CardField) {
	printCard(DangerBoldText(strings.TrimSpace(title)), fields)
}

func printCard(title string, fields []CardField) {
	fmt.Println(PrimaryBoldText("╭─ ServerTool"))
	fmt.Println(PrimaryText("│") + " " + title)
	for _, field := range fields {
		label := strings.TrimSpace(field.Label)
		value := strings.TrimSpace(field.Value)
		if label == "" {
			fmt.Println(PrimaryText("│") + " " + value)
		} else {
			fmt.Println(PrimaryText("│") + " " + InfoText(label+"：") + value)
		}
		if detail := strings.TrimSpace(field.Detail); detail != "" {
			fmt.Println(PrimaryText("│") + "   " + MutedText(detail))
		}
	}
	fmt.Println(PrimaryText("╰" + strings.Repeat("─", 46)))
}

// PrintField applies the shared field-label color outside a card.
func PrintField(label, value string) {
	fmt.Println(InfoText(strings.TrimSpace(label)+"：") + value)
}

// TableCell pads colored terminal text by visible width so ANSI sequences do
// not break table alignment.
func TableCell(text string, width int) string {
	padding := width - displayWidth(text)
	if padding < 0 {
		padding = 0
	}
	return text + strings.Repeat(" ", padding)
}

// StatusBadge colors report states consistently by their meaning.
func StatusBadge(text string) string {
	color := yellow
	switch strings.TrimSpace(text) {
	case "安全":
		color = green
	case "风险":
		color = red
	case "信息":
		color = blue
	}
	return paint(color, "["+text+"]")
}

func menuKey(key, style string) string {
	return paint(style, fmt.Sprintf("%-3s", strings.TrimSpace(key)))
}

func padDisplay(text string, width int) string {
	padding := width - displayWidth(text)
	if padding < 1 {
		padding = 1
	}
	return text + strings.Repeat(" ", padding)
}

func displayWidth(text string) int {
	width := 0
	inEscape := false
	for _, current := range text {
		if inEscape {
			if current == 'm' {
				inEscape = false
			}
			continue
		}
		if current == '\x1b' {
			inEscape = true
			continue
		}
		if current >= 0x2e80 {
			width += 2
		} else {
			width++
		}
	}
	return width
}

func normalizePrompt(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if strings.HasSuffix(prompt, ":") {
		prompt = strings.TrimSuffix(prompt, ":") + "："
	}
	return prompt
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
