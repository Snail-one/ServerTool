package unattended

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"snail_tool/internal/ui"
)

const logTailBytes = 256 * 1024

type updateLogPaths struct {
	main, dpkg string
}

func (service manager) logPaths() (updateLogPaths, error) {
	output, err := service.query("apt-config", "shell", "LOG_DIR", "Unattended-Upgrade::LogDir/d",
		"LOG_FILE", "Unattended-Upgrade::LogFile", "LEGACY_DIR", "APT::UnattendedUpgrades::LogDir/d",
		"LEGACY_FILE", "APT::UnattendedUpgrades::LogFile")
	if err != nil {
		return updateLogPaths{}, err
	}
	values := assignments(output)
	dir := values["LOG_DIR"]
	if dir == "" {
		dir = values["LEGACY_DIR"]
	}
	if dir == "" {
		dir = "/var/log/unattended-upgrades"
	}
	main := values["LOG_FILE"]
	if main == "" {
		main = values["LEGACY_FILE"]
	}
	if main == "" {
		main = "unattended-upgrades.log"
	}
	if !filepath.IsAbs(main) {
		main = filepath.Join(dir, main)
	}
	return updateLogPaths{main: main, dpkg: filepath.Join(dir, "unattended-upgrades-dpkg.log")}, nil
}

type updateRun struct {
	start, result, reason string
	packages              []string
	simulated             bool
	confirmedPackages     bool
}

type updateHistory struct {
	timedRun ui.CardField
	run      updateRun
	logError error
}

func (service manager) updateHistory() updateHistory {
	history := updateHistory{timedRun: service.lastTimedRun()}
	paths, err := service.logPaths()
	if err != nil {
		history.logError = err
		return history
	}
	current, err := readLogTail(paths.main)
	rotated, rotatedErr := readLogTail(paths.main + ".1")
	history.run = latestUpdateRun(rotated + "\n" + current)
	history.logError = errors.Join(err, rotatedErr)
	return history
}

func (history updateHistory) fields(full bool) []ui.CardField {
	run := history.run
	record := ui.CardField{Label: "最近更新记录", Value: "暂无记录"}
	switch {
	case run.start != "":
		result := run.result
		if result == "" {
			result = "暂无可确认的安装结果"
		}
		record.Value = run.start + " · " + result
	case history.logError != nil:
		record.Value = "日志读取失败"
		record.Detail = cleanLogText(history.logError.Error(), 200)
	}
	fields := []ui.CardField{history.timedRun, record}
	if run.reason != "" {
		fields = append(fields, ui.CardField{Label: "日志报错", Value: cleanLogText(run.reason, 220)})
	}
	if run.confirmedPackages && len(run.packages) > 0 {
		packages := run.packages
		suffix := ""
		if !full && len(packages) > 6 {
			packages = packages[:6]
			suffix = fmt.Sprintf(" 等 %d 个（按 8 查看）", len(run.packages))
		}
		fields = append(fields, ui.CardField{Label: "已升级软件包", Value: strings.Join(packages, "、") + suffix})
	}
	return fields
}

func (service manager) lastTimedRun() ui.CardField {
	field := ui.CardField{Label: "最近一次定时服务", Value: "暂无执行记录"}
	if !service.exists("systemctl") {
		field.Value = "未找到 systemctl"
		return field
	}
	output, err := service.query("systemctl", "show",
		"--property=ExecMainStartTimestamp,ExecMainExitTimestamp,ActiveState,Result,ExecMainStatus", upgradeUnit)
	if err != nil {
		field.Value = "状态读取失败"
		return field
	}
	state := assignments(output)
	start := triggerLabel(state["ExecMainStartTimestamp"])
	if start == "暂无" {
		return field
	}
	switch {
	case state["ActiveState"] == "activating" || state["ActiveState"] == "active":
		field.Value = start + " · 运行中"
	case state["Result"] != "" && state["Result"] != "success":
		field.Value = start + " · 执行失败"
		field.Detail = "systemd 结果：" + state["Result"] + " · 退出状态：" + state["ExecMainStatus"]
	case state["Result"] == "success" && triggerLabel(state["ExecMainExitTimestamp"]) != "暂无":
		field.Value = start + " · 服务已结束"
	default:
		field.Value = start + " · 执行结果未知"
	}
	return field
}

func readLogTail(path string) (string, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("日志不是普通文件: %s", path)
	}
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer file.Close()
	start := max(int64(0), info.Size()-logTailBytes)
	data, err := io.ReadAll(io.NewSectionReader(file, start, logTailBytes))
	if err != nil {
		return "", err
	}
	log := string(data)
	if start > 0 {
		_, log, _ = strings.Cut(log, "\n")
	}
	return log, nil
}

var unattendedLogLine = regexp.MustCompile(`^(\d{4}-\d\d-\d\d \d\d:\d\d:\d\d)(?:[,.]\d+)?\s+([A-Z]+)\s+(.*)$`)

func latestUpdateRun(log string) updateRun {
	var current, last, lastResult updateRun
	save := func() {
		if current.start == "" || current.simulated {
			return
		}
		last = current
		if current.result != "" {
			lastResult = current
		}
	}
	for _, line := range strings.Split(log, "\n") {
		match := unattendedLogLine.FindStringSubmatch(strings.TrimSuffix(line, "\r"))
		if match == nil {
			continue
		}
		stamp, level, message := match[1], match[2], match[3]
		switch {
		case message == "Starting unattended upgrades script":
			save()
			current = updateRun{start: stamp}
		case current.start == "":
			continue
		case strings.Contains(message, "Option --dry-run given"), strings.Contains(message, "dry-run mode"),
			strings.HasPrefix(message, "Configured to install on shutdown"):
			current.simulated = true
		case strings.HasPrefix(message, "Packages that will be upgraded:"):
			current.packages = strings.Fields(cleanLogText(strings.TrimPrefix(message, "Packages that will be upgraded:"), 0))
		case strings.HasPrefix(message, "All upgrades installed"):
			current.result = "升级已安装"
			current.confirmedPackages = true
		case strings.HasPrefix(message, "Packages that were upgraded:"):
			current.packages = strings.Fields(cleanLogText(strings.TrimPrefix(message, "Packages that were upgraded:"), 0))
			current.confirmedPackages = true
			if current.result == "" {
				current.result = "已记录软件包升级"
			}
		case strings.HasPrefix(message, "No packages found that can be upgraded unattended"):
			if current.result == "" {
				current.result = "未发现可升级软件包"
			}
		case level == "ERROR" || level == "CRITICAL":
			if current.result != "升级已安装" {
				current.result = "执行报错"
			}
			if current.reason == "" || strings.HasPrefix(strings.ToLower(message), "error message:") {
				current.reason = message
			}
		}
	}
	save()
	if lastResult.start != "" {
		return lastResult
	}
	return last
}

func cleanLogText(text string, limit int) string {
	text = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, text))
	if limit > 0 {
		if runes := []rune(text); len(runes) > limit {
			return string(runes[:limit]) + "…"
		}
	}
	return text
}

func (service manager) logMenu(view *ui.UI) error {
	choices := map[string]string{"1": "main", "2": "dpkg", "3": "journal"}
	for {
		history := service.updateHistory()
		draw := func() {
			showPage("最近更新日志")
			ui.PrintInfoCard("最近更新结果", history.fields(true)...)
			fmt.Println()
			ui.MenuOption("1", "查看更新主日志")
			ui.MenuOption("2", "查看 dpkg 安装日志")
			ui.MenuOption("3", "查看定时服务日志")
			ui.MenuExit("0/q", "返回自动更新管理")
		}
		choice, err := ui.AskMenuChoice(view, "请选择", "", choices, draw)
		if err != nil {
			return err
		}
		ui.RunAction(view, "读取日志失败，已返回日志菜单", func() error {
			if choice == "journal" {
				showPage("定时服务日志")
				return service.run("journalctl", "--no-pager", "-u", upgradeUnit, "-n", "60")
			}
			paths, err := service.logPaths()
			if err != nil {
				return err
			}
			path, title := paths.main, "更新主日志"
			if choice == "dpkg" {
				path, title = paths.dpkg, "dpkg 安装日志"
			}
			return service.showLogTail(title, path)
		})
	}
}

func (service manager) showLogTail(title, path string) error {
	showPage(title)
	info, err := os.Stat(path)
	if os.IsNotExist(err) || (err == nil && info.Size() == 0) {
		info, err = os.Stat(path + ".1")
		if os.IsNotExist(err) {
			ui.PrintInfoCard("暂无日志", ui.CardField{Label: "文件", Value: path})
			return nil
		}
		path += ".1"
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("日志不是普通文件: %s", path)
	}
	ui.PrintInfoCard("日志来源", ui.CardField{Label: "文件", Value: path}, ui.CardField{Label: "范围", Value: "最近 60 行"})
	fmt.Println()
	return service.run("tail", "-n", "60", "--", path)
}
