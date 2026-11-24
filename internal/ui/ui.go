package ui

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"snail_tool/internal/input"
)

const (
	reset     = "\033[0m"
	bold      = "\033[1m"
	dim       = "\033[2m"
	orange    = "\033[38;5;208m"
	blue      = "\033[34m"
	green     = "\033[32m"
	yellow    = "\033[33m"
	red       = "\033[31m"
	gray      = "\033[90m"
	lightBlue = "\033[94m"
	white     = "\033[97m"
)

var (
	terminalStyle = regexp.MustCompile(`\x1b\[[0-9;:]*m`)
	cardValuePart = regexp.MustCompile(`[^·，\n]+`)
)

const (
	cardTonePlain = iota
	cardToneInactive
	cardToneUncertain
	cardToneFailure
)

// cardStatusWords are short state phrases. A longer explanatory sentence that
// merely contains one of these words stays white.
var cardStatusWords = []struct {
	word string
	tone int
}{
	{"关闭", cardToneInactive},
	{"已关闭", cardToneInactive},
	{"未开启", cardToneInactive},
	{"未启用", cardToneInactive},
	{"未生效", cardToneInactive},
	{"已停用", cardToneInactive},
	{"已禁用", cardToneInactive},
	{"已拦截", cardToneInactive},
	{"已禁止", cardToneInactive},
	{"已停止", cardToneInactive},
	{"已暂停", cardToneInactive},
	{"未运行", cardToneInactive},
	{"未启动", cardToneInactive},
	{"未设置", cardToneInactive},
	{"未配置", cardToneInactive},
	{"未安装", cardToneInactive},
	{"未接管", cardToneInactive},
	{"未找到", cardToneInactive},
	{"未发现", cardToneInactive},
	{"未检测到", cardToneInactive},
	{"已屏蔽", cardToneInactive},
	{"无容器", cardToneInactive},
	{"未限制", cardToneInactive},
	{"暂无", cardToneInactive},
	{"无法确认", cardToneUncertain},
	{"无法判断", cardToneUncertain},
	{"无法检测", cardToneUncertain},
	{"无法比较", cardToneUncertain},
	{"无法读取", cardToneUncertain},
	{"无法解析", cardToneUncertain},
	{"无法获取", cardToneUncertain},
	{"无法识别", cardToneUncertain},
	{"未知", cardToneUncertain},
	{"不支持", cardToneUncertain},
	{"不兼容", cardToneUncertain},
	{"部分开启", cardToneUncertain},
	{"部分开放", cardToneUncertain},
	{"部分接管", cardToneUncertain},
	{"部分运行", cardToneUncertain},
	{"取决于软件包来源", cardToneUncertain},
	{"由自定义 APT 规则决定", cardToneUncertain},
	{"未同步", cardToneUncertain},
	{"规则未同步", cardToneUncertain},
	{"等待", cardToneUncertain},
	{"未识别", cardToneUncertain},
	{"正在启动", cardToneUncertain},
	{"正在停止", cardToneUncertain},
	{"配置读取失败", cardToneFailure},
	{"状态获取失败", cardToneFailure},
	{"检测失败", cardToneFailure},
	{"读取失败", cardToneFailure},
	{"校验失败", cardToneFailure},
	{"加载失败", cardToneFailure},
	{"保存失败", cardToneFailure},
	{"执行失败", cardToneFailure},
	{"检查失败", cardToneFailure},
	{"运行失败", cardToneFailure},
	{"失败", cardToneFailure},
}

type UI struct {
	reader     *bufio.Reader
	stdin      *os.File
	inputEnded bool
}

// CardField is one labeled value in a terminal information card. Detail is
// rendered on a muted continuation line and is useful for paths or directives.
// Changed marks a proposed or pending change to the field value.
type CardField struct {
	Label   string
	Value   string
	Detail  string
	Changed bool
}

func New() *UI {
	return &UI{reader: bufio.NewReader(os.Stdin), stdin: os.Stdin}
}

// InputEnded reports whether standard input reached EOF.
func (u *UI) InputEnded() bool { return u.inputEnded }

func (u *UI) Ask(prompt string) (string, error) {
	return ask(prompt, u.ReadLine)
}

// AskSecret reads and validates a response with terminal echo disabled.
func (u *UI) AskSecret(prompt string) (string, error) {
	restore, err := hideInputEcho(u.stdin)
	if err != nil {
		return "", fmt.Errorf("无法隐藏终端输入: %w", err)
	}
	if restore == nil {
		return u.Ask(prompt)
	}
	defer restore()
	return ask(prompt, func() (string, error) {
		value, err := u.ReadLine()
		fmt.Println()
		return value, err
	})
}

func ask(prompt string, readLine func() (string, error)) (string, error) {
	prompt = normalizePrompt(prompt)
	for {
		fmt.Print(paint(bold+orange, "❯ ") + paint(bold, prompt) + " ")
		value, err := readLine()
		if !errors.Is(err, input.ErrInvalid) {
			return value, err
		}
		fmt.Println(WarningText(err.Error()))
	}
}

// ReadLine consumes one response without printing over a refreshing display.
func (u *UI) ReadLine() (string, error) {
	var line []byte
	tooLong := false
	for {
		part, err := u.reader.ReadSlice('\n')
		if !tooLong {
			if len(line)+len(part) > input.MaxLength+2 {
				tooLong = true
				line = nil
			} else {
				line = append(line, part...)
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				u.inputEnded = true
			}
			return "", err
		}
		break
	}
	if tooLong {
		return "", fmt.Errorf("%w：单行内容不能超过 64 KiB", input.ErrInvalid)
	}
	value := strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r")
	if err := input.Text(value); err != nil {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func (u *UI) Confirm(prompt string) (bool, error) {
	for {
		value, err := u.Ask(prompt)
		if err != nil {
			return false, err
		}
		if IsReturnChoice(value) {
			return false, ErrReturnToMenu
		}
		switch strings.ToLower(value) {
		case "y", "yes":
			return true, nil
		case "", "n", "no":
			return false, nil
		default:
			fmt.Println(WarningText("请输入 y 或 n，或输入 0/q 返回"))
		}
	}
}

func (u *UI) Pause() {
	u.PauseWithPrompt("按回车返回菜单…")
}

func (u *UI) PauseWithPrompt(prompt string) {
	if u.InputEnded() {
		return
	}
	prompt = strings.TrimSpace(prompt)
	if strings.HasSuffix(prompt, "...") {
		prompt = strings.TrimSuffix(prompt, "...") + "…"
	}
	fmt.Println()
	fmt.Print(paint(dim, prompt) + " ")
	_, _ = u.ReadLine()
	fmt.Println()
}

func ClearScreen() {
	if !canClearScreen() {
		return
	}
	// \033[2J 清屏, \033[H 光标移到左上角, \033[3J 尝试清除回滚缓冲 (部分终端支持)
	fmt.Print("\033[2J\033[H\033[3J")
}

func canClearScreen() bool {
	return terminalOutput() && !strings.EqualFold(os.Getenv("TERM"), "dumb")
}

// MenuTitle prints a consistent, color-aware breadcrumb for terminal menus.
func MenuTitle(parts ...string) {
	printMenuTitle(parts...)
	fmt.Println()
}

// MenuExitPage renders a title followed by a return row on pages without a menu body.
func MenuExitPage(key, label string, title ...string) {
	printMenuTitle(title...)
	MenuExit(key, label)
}

func printMenuTitle(parts ...string) {
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
	menuOptionHint(key, label, hint, bold+blue)
}

// MenuShortcutHint highlights a shortcut in the primary orange color.
func MenuShortcutHint(key, label, hint string) {
	menuOptionHint(key, label, hint, bold+orange)
}

func menuOptionHint(key, label, hint, style string) {
	hint = strings.TrimSpace(hint)
	if hint == "" {
		fmt.Printf("  %s %s\n", menuKey(key, style), label)
		return
	}
	fmt.Printf("  %s %s%s\n", menuKey(key, style), padDisplay(label, 22), paint(gray, "-- "+hint))
}

// MenuOptionStatus keeps status badges in a fixed column across menu rows.
func MenuOptionStatus(key, label, status string) {
	menuOptionStatusHint(key, label, status, "", bold+blue)
}

// MenuOptionStatusHint combines the global status column with a gray detail
// column. It is useful when an item has both state and a managed path.
func MenuOptionStatusHint(key, label, status, hint string) {
	menuOptionStatusHint(key, label, status, hint, bold+blue)
}

func MenuShortcutStatusHint(key, label, status, hint string) {
	menuOptionStatusHint(key, label, status, hint, bold+orange)
}

func menuOptionStatusHint(key, label, status, hint, style string) {
	status = strings.TrimSpace(status)
	hint = strings.TrimSpace(hint)
	if hint == "" {
		if status == "" {
			menuOptionHint(key, label, "", style)
		} else {
			fmt.Printf("  %s %s%s\n", menuKey(key, style), padDisplay(label, 18), status)
		}
		return
	}
	fmt.Printf(
		"  %s %s%s%s\n",
		menuKey(key, style),
		padDisplay(label, 18),
		padDisplay(status, 16),
		paint(gray, "-- "+hint),
	)
}

// MenuExit renders the return/exit row separately from regular actions.
func MenuExit(key, label string) {
	fmt.Printf("\n  %s %s\n\n", menuKey(key, bold+yellow), paint(dim, label))
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

// InstallationBadge distinguishes installed software from missing optional
// software without using the success/warning colors reserved for outcomes.
func InstallationBadge(installed bool) string {
	if installed {
		return SoftwareBadge("已安装", true)
	}
	return SoftwareBadge("未安装", false)
}

// SoftwareBadge colors an installed software name or count orange and a
// missing software state gray.
func SoftwareBadge(text string, installed bool) string {
	return StateText("["+text+"]", installed)
}

func CurrentBadge(current, enabled bool) string {
	if current {
		return SoftwareBadge("当前", enabled)
	}
	return ""
}

func SwitchBadge(enabled bool) string {
	if enabled {
		return SoftwareBadge("已开启", true)
	}
	return SoftwareBadge("已关闭", false)
}

// ConfiguredBadge keeps configuration status wording and color consistent.
func ConfiguredBadge(configured bool) string {
	if configured {
		return ConfigurationBadge("已配置", true)
	}
	return ConfigurationBadge("未配置", false)
}

// ConfigurationBadge colors configured state orange and unconfigured state
// gray, matching installation-state badges.
func ConfigurationBadge(text string, configured bool) string {
	return SoftwareBadge(text, configured)
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
	return paint(orange, text)
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

// StateText highlights an active setting and mutes other settings.
func StateText(text string, active bool) string {
	if active {
		return PrimaryText(text)
	}
	return MutedText(text)
}

// PrintInfoCard and its variants put the card title in the top border.
// Labeled values use the card palette independently of the caller's styling.
func PrintInfoCard(title string, fields ...CardField) {
	printCard(title, orange, fields)
}

// PrintSectionCard prints an information card for a section of a report.
func PrintSectionCard(title string, fields ...CardField) {
	PrintInfoCard(title, fields...)
}

func PrintSuccessCard(title string, fields ...CardField) {
	printCard(title, green, fields)
}

func PrintWarningCard(title string, fields ...CardField) {
	printCard(title, yellow, fields)
}

func PrintDangerCard(title string, fields ...CardField) {
	printCard(title, red, fields)
}

func printCard(title, titleColor string, fields []CardField) {
	fmt.Println(paint(bold+titleColor, "╭─ "+strings.TrimSpace(title)))
	for _, field := range fields {
		label := strings.TrimSpace(field.Label)
		value := strings.TrimSpace(field.Value)
		if label == "" {
			fmt.Println(PrimaryText("│") + " " + value)
		} else {
			fmt.Println(PrimaryText("│") + " " + InfoText(label+"：") + cardValue(value, field.Changed))
		}
		if detail := strings.TrimSpace(field.Detail); detail != "" {
			fmt.Println(PrimaryText("│") + "   " + MutedText(detail))
		}
	}
	fmt.Println(PrimaryText("╰" + strings.Repeat("─", 46)))
}

func cardValue(value string, changed bool) string {
	value = strings.TrimSpace(terminalStyle.ReplaceAllString(value, ""))
	if value == "" || !colorEnabled() {
		return value
	}
	if changed {
		return lightBlue + value + reset
	}
	value = cardValuePart.ReplaceAllStringFunc(value, func(part string) string {
		switch cardTone(strings.TrimSpace(part)) {
		case cardToneInactive:
			return gray + part + white
		case cardToneUncertain:
			return yellow + part + white
		case cardToneFailure:
			return red + part + white
		default:
			return part
		}
	})
	return white + value + reset
}

func cardTone(status string) int {
	status = strings.TrimSpace(status)
	if strings.HasPrefix(status, "[") && strings.HasSuffix(status, "]") {
		if tone := cardTone(status[1 : len(status)-1]); tone != cardTonePlain {
			return tone
		}
	}
	if bare, ok := stripStatusParenthetical(status); ok {
		if tone := cardToneCore(bare); tone != cardTonePlain {
			return tone
		}
	}
	if tone := cardToneCore(status); tone != cardTonePlain {
		return tone
	}
	return embeddedStatusTone(status)
}

func cardToneCore(status string) int {
	status = strings.TrimSpace(status)
	if tone := statusWordTone(stripStatusScope(status)); tone != cardTonePlain {
		return tone
	}
	return statusWordTone(status)
}

func stripStatusScope(status string) string {
	for _, prefix := range []string{"IPv4", "IPv6", "公网", "局域网", "入站", "出站"} {
		if !strings.HasPrefix(status, prefix) {
			continue
		}
		rest := strings.TrimPrefix(status, prefix)
		if rest == "" {
			continue
		}
		first, _ := utf8.DecodeRuneInString(rest)
		if first != ' ' && !isHan(first) {
			continue
		}
		rest = strings.TrimSpace(rest)
		if rest != "" {
			return rest
		}
	}
	return status
}

func stripStatusParenthetical(status string) (string, bool) {
	for _, pair := range [][2]string{{"（", "）"}, {"(", ")"}} {
		if !strings.HasSuffix(status, pair[1]) {
			continue
		}
		start := strings.LastIndex(status, pair[0])
		if start <= 0 {
			return status, false
		}
		return strings.TrimSpace(status[:start]), true
	}
	return status, false
}

func statusWordTone(status string) int {
	bestRunes, bestTone := 0, cardTonePlain
	for _, item := range cardStatusWords {
		runes := utf8.RuneCountInString(item.word)
		if runes < bestRunes {
			continue
		}
		matched := false
		switch {
		case status == item.word:
			matched = true
		case strings.HasPrefix(status, item.word) && shortStatusTail(status[len(item.word):]):
			matched = true
		case strings.HasSuffix(status, item.word) && shortStatusHead(status[:len(status)-len(item.word)]):
			matched = true
		}
		if matched && (runes > bestRunes || (runes == bestRunes && item.tone > bestTone)) {
			bestRunes, bestTone = runes, item.tone
		}
	}
	return bestTone
}

func embeddedStatusTone(status string) int {
	if strings.ContainsAny(status, "；。！？，、") || utf8.RuneCountInString(status) > 24 {
		return cardTonePlain
	}
	bestRunes, bestTone := 0, cardTonePlain
	for _, item := range cardStatusWords {
		index := strings.Index(status, item.word)
		if index < 0 {
			continue
		}
		prefix, suffix := status[:index], status[index+len(item.word):]
		if utf8.RuneCountInString(strings.TrimSpace(prefix)) > 8 || utf8.RuneCountInString(strings.TrimSpace(suffix)) > 12 {
			continue
		}
		if strings.ContainsAny(prefix+suffix, "；。！？，、") {
			continue
		}
		runes := utf8.RuneCountInString(item.word)
		if runes > bestRunes || (runes == bestRunes && item.tone > bestTone) {
			bestRunes, bestTone = runes, item.tone
		}
	}
	return bestTone
}

func shortStatusTail(rest string) bool {
	rest = strings.TrimSpace(rest)
	return rest == "" || (!strings.ContainsAny(rest, "；。！？，、") && utf8.RuneCountInString(rest) <= 12)
}

func shortStatusHead(prefix string) bool {
	prefix = strings.TrimSpace(prefix)
	return prefix == "" || (!strings.ContainsAny(prefix, "；。！？，、") && utf8.RuneCountInString(prefix) <= 12)
}

func isHan(r rune) bool {
	return r >= 0x2e80
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
		color = orange
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
