package unattended

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"snail_tool/internal/input"
	"snail_tool/internal/ui"
)

const timerHeader = "# Managed by ServerTool.\n"

func (service manager) scheduleMenu(view *ui.UI) error {
	for {
		ui.ClearScreen()
		refresh := service.scheduleOverview(refreshTimer)
		upgrade := service.scheduleOverview(upgradeTimer)
		draw := func() {
			ui.MenuTitle("系统工具", Title, "定时器")
			ui.PrintInfoCard("索引刷新", refresh...)
			fmt.Println()
			ui.PrintInfoCard("自动更新", upgrade...)
			fmt.Println()
			ui.MenuOption("1", "调整索引刷新定时器")
			ui.MenuOption("2", "调整自动更新定时器")
			fmt.Println()
			ui.MenuOption("3", "恢复索引刷新原设置")
			ui.MenuOption("4", "恢复自动更新原设置")
			ui.MenuExit("0/q", "返回自动更新管理")
		}
		choice, err := ui.AskMenuChoice(view, "请选择", "", map[string]string{"1": "1", "2": "2", "3": "3", "4": "4"}, draw)
		if err != nil {
			return err
		}
		name := refreshTimer
		if choice == "2" || choice == "4" {
			name = upgradeTimer
		}
		ui.RunAction(view, "修改定时器失败", func() error {
			return service.editSchedule(view, name, choice == "3" || choice == "4")
		})
	}
}

func (service manager) editSchedule(view *ui.UI, name string, reset bool) error {
	if !service.root() {
		return fmt.Errorf("修改定时器需要 root 权限")
	}
	state, err := service.timer(name)
	if err != nil {
		return err
	}
	if state.load != "loaded" {
		return fmt.Errorf("定时器 %s 不可用（%s）", name, state.load)
	}
	path := filepath.Join(service.timerDir, name+".d", "90-servertool-schedule.conf")
	previous, exists, err := readManaged(path, timerHeader)
	if err != nil {
		return err
	}
	if reset {
		showPage("恢复" + timerLabel(name) + "定时器")
		if !exists {
			ui.PrintInfoCard("当前使用原有定时器设置")
			return nil
		}
		ui.PrintInfoCard("将移除的定时配置", ui.CardField{Label: "定时器", Value: name}, ui.CardField{Label: "文件", Value: path})
		fmt.Println()
		if err := confirm(view, "确认移除本工具的定时设置？(y/N)："); err != nil {
			return err
		}
		return service.publishSchedule(name, path, previous, exists, "")
	}
	if !service.exists("systemd-analyze") {
		return fmt.Errorf("定时设置需要 systemd-analyze 校验日历表达式")
	}
	showPage("调整" + timerLabel(name) + "定时器")
	ui.PrintInfoCard("当前定时设置", state.scheduleFields(name)...)
	fmt.Println()
	var calendar string
	var delay, accuracy int
	var persistent string
	err = ui.RunPromptSteps(
		ui.PromptStep{Run: func() error {
			calendar, err = view.Ask("触发时间（例如 *-*-* 03:30:00 或 Mon *-*-* 03:30:00，0/q 返回）：")
			if err != nil {
				return err
			}
			if ui.IsReturnChoice(calendar) {
				return ui.ErrReturnToMenu
			}
			if calendar == "" || strings.HasSuffix(calendar, "\\") || strings.ContainsAny(calendar, "%\r\n") {
				return fmt.Errorf("请输入有效的 systemd 日历表达式")
			}
			if _, err := service.query("systemd-analyze", "calendar", "--", calendar); err != nil {
				return fmt.Errorf("日历表达式无效: %w", err)
			}
			return nil
		}},
		ui.PromptStep{Run: func() error {
			delay, err = askSeconds(view, "最大随机延迟（秒，回车或 0 表示关闭，q 返回）：", "0", 0)
			return err
		}},
		ui.PromptStep{Run: func() error {
			persistent, err = askToggle(view, switchLabel(state.persistent), "true", "false", "2", func() {
				ui.MenuSection("补跑关机期间错过的任务")
			})
			return err
		}},
		ui.PromptStep{Run: func() error {
			accuracy, err = askSeconds(view, "定时器触发精度（秒，回车使用 60 秒，0/q 返回）：", "60", 1)
			return err
		}},
	)
	if err != nil {
		return err
	}
	content := timerHeader + "[Timer]\nOnCalendar=\nOnCalendar=" + calendar +
		"\nRandomizedDelaySec=" + strconv.Itoa(delay) + "s\nAccuracySec=" + strconv.Itoa(accuracy) +
		"s\nPersistent=" + persistent + "\n"
	ui.PrintInfoCard("将应用的定时设置",
		ui.CardField{Label: "定时器", Value: name},
		ui.CardField{Label: "触发时间", Value: calendar, Changed: !sameCalendar(state.calendar, calendar)},
		ui.CardField{Label: "最大随机延迟", Value: fmt.Sprintf("%d 秒", delay), Changed: !sameDurationSeconds(state.delay, delay)},
		ui.CardField{Label: "触发精度", Value: fmt.Sprintf("%d 秒", accuracy), Changed: !sameDurationSeconds(state.accuracy, accuracy)},
		ui.CardField{Label: "补跑错过的任务", Value: switchLabel(persistent), Changed: switchLabel(persistent) != switchLabel(state.persistent)})
	if err := confirm(view, "确认保存并重载定时器？(y/N)："); err != nil {
		return err
	}
	if err := os.Mkdir(filepath.Dir(path), 0755); err != nil && !os.IsExist(err) {
		return err
	}
	return service.publishSchedule(name, path, previous, exists, content)
}

func askSeconds(view *ui.UI, prompt, fallback string, minimum int) (int, error) {
	value, err := view.Ask(prompt)
	if err != nil {
		return 0, err
	}
	if ui.IsReturnChoice(value) && !(value == "0" && minimum == 0) {
		return 0, ui.ErrReturnToMenu
	}
	if value == "" {
		value = fallback
	}
	return input.Number(value, minimum, 2147483647)
}

func (service manager) scheduleOverview(name string) []ui.CardField {
	state, err := service.timer(name)
	if err != nil {
		return []ui.CardField{{Label: "系统单元", Value: name}, {Label: "读取失败", Value: err.Error()}}
	}
	return state.scheduleFields(name)
}

func (state timer) scheduleFields(name string) []ui.CardField {
	fields := []ui.CardField{
		{Label: "系统单元", Value: name},
		{Label: "状态", Value: state.statusLabel()},
	}
	if state.load != "loaded" {
		return fields
	}
	calendar := state.calendar
	if calendar == "" {
		calendar = "未设置"
	}
	return append(fields,
		ui.CardField{Label: "日历规则", Value: calendar},
		ui.CardField{Label: "下次触发", Value: triggerLabel(state.next)},
		ui.CardField{Label: "随机延迟", Value: state.delay},
		ui.CardField{Label: "触发精度", Value: state.accuracy},
		ui.CardField{Label: "补跑错过的任务", Value: switchLabel(state.persistent)},
	)
}

func (service manager) publishSchedule(name, path string, previous []byte, exists bool, content string) error {
	reload := func() error {
		if err := service.run("systemctl", "daemon-reload"); err != nil {
			return err
		}
		return service.run("systemctl", "try-restart", name)
	}
	attempted := false
	err := publish(path, previous, exists, content, func() error {
		attempted = true
		return reload()
	})
	if err != nil && attempted && checkSnapshot(path, previous, exists) == nil {
		return errors.Join(err, reload())
	}
	return err
}

func sameCalendar(current, next string) bool {
	next = strings.TrimSpace(next)
	if next == "" {
		return false
	}
	current = strings.TrimSpace(current)
	if current == next {
		return true
	}
	found := onCalendars(current)
	return len(found) == 1 && found[0] == next
}

func onCalendars(value string) []string {
	var found []string
	for _, piece := range strings.FieldsFunc(value, func(r rune) bool { return r == ';' || r == '\n' }) {
		piece = strings.Trim(piece, " {}\t")
		expr, ok := strings.CutPrefix(piece, "OnCalendar=")
		if !ok {
			continue
		}
		expr = strings.TrimSpace(expr)
		if expr != "" {
			found = append(found, expr)
		}
	}
	return found
}

var durationPattern = regexp.MustCompile(`(\d+)\s*(usec|us|msec|ms|seconds|second|sec|s|minutes|minute|min|m|hours|hour|hr|h|days|day|d|weeks|week|w)`)

var durationMicros = map[string]int64{
	"us": 1, "usec": 1,
	"ms": 1000, "msec": 1000,
	"s": 1000000, "sec": 1000000, "second": 1000000, "seconds": 1000000,
	"m": 60 * 1000000, "min": 60 * 1000000, "minute": 60 * 1000000, "minutes": 60 * 1000000,
	"h": 3600 * 1000000, "hr": 3600 * 1000000, "hour": 3600 * 1000000, "hours": 3600 * 1000000,
	"d": 86400 * 1000000, "day": 86400 * 1000000, "days": 86400 * 1000000,
	"w": 604800 * 1000000, "week": 604800 * 1000000, "weeks": 604800 * 1000000,
}

func sameDurationSeconds(current string, seconds int) bool {
	got, ok := systemdSeconds(current)
	return ok && got == seconds
}

func systemdSeconds(value string) (int, bool) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return 0, false
	}
	if n, err := strconv.ParseInt(value, 10, 64); err == nil {
		if n == 0 {
			return 0, true
		}
		if n < 0 || n%1000000 != 0 {
			return 0, false
		}
		return int(n / 1000000), true
	}
	matches := durationPattern.FindAllStringSubmatchIndex(value, -1)
	if len(matches) == 0 {
		return 0, false
	}
	var total int64
	pos := 0
	for _, match := range matches {
		if strings.TrimSpace(value[pos:match[0]]) != "" {
			return 0, false
		}
		number, err := strconv.ParseInt(value[match[2]:match[3]], 10, 64)
		if err != nil {
			return 0, false
		}
		total += number * durationMicros[value[match[4]:match[5]]]
		pos = match[1]
	}
	if strings.TrimSpace(value[pos:]) != "" || total < 0 || total%1000000 != 0 {
		return 0, false
	}
	return int(total / 1000000), true
}
