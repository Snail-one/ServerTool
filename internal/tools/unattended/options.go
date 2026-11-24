package unattended

import (
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"snail_tool/internal/ui"
)

type aptOption struct {
	label, key, kind, fallback string
}

var rebootOption = aptOption{"自动重启", rebootKey, "toggle", "false"}
var oldKernelOption = aptOption{"移除不再使用的内核包", "Unattended-Upgrade::Remove-Unused-Kernel-Packages", "toggle", "true"}

var policyOptions = []aptOption{
	{"软件包索引刷新频率", refreshKey, "interval", "0"},
	{"自动更新频率", upgradeKey, "frequency", "1"},
	{"预下载频率", downloadKey, "interval", "0"},
	{"APT 定时任务总开关", "APT::Periodic::Enable", "numeric toggle", "1"},
	rebootOption,
	{"允许有用户时重启", "Unattended-Upgrade::Automatic-Reboot-WithUsers", "toggle", "true"},
	{"自动重启时间", "Unattended-Upgrade::Automatic-Reboot-Time", "reboot time", "now"},
	{"关机时安装更新", shutdownKey, "toggle", "false"},
	{"修复中断的 dpkg", "Unattended-Upgrade::AutoFixInterruptedDpkg", "toggle", "true"},
	{"仅接通交流电时更新", "Unattended-Upgrade::OnlyOnACPower", "toggle", "true"},
	{"严格匹配允许名单", "Unattended-Upgrade::Package-Whitelist-Strict", "toggle", "false"},
	{"按最小步骤升级", "Unattended-Upgrade::MinimalSteps", "toggle", "true"},
	oldKernelOption,
	{"移除新产生的无用依赖", "Unattended-Upgrade::Remove-New-Unused-Dependencies", "toggle", "true"},
	{"移除所有不再使用的依赖", "Unattended-Upgrade::Remove-Unused-Dependencies", "toggle", "false"},
	{"报告收件地址", "Unattended-Upgrade::Mail", "mail", ""},
	{"发送报告条件", "Unattended-Upgrade::MailReport", "report", ""},
}

var policySections = map[string]string{
	refreshKey: "周期与调度",
	rebootKey:  "重启与关机",
	"Unattended-Upgrade::AutoFixInterruptedDpkg": "升级与清理",
	"Unattended-Upgrade::Mail":                   "邮件报告",
}

func (service manager) optionMenu(view *ui.UI) error {
	choices := make(map[string]string, len(policyOptions))
	for index := range policyOptions {
		key := strconv.Itoa(index + 1)
		choices[key] = key
	}
	for {
		ui.ClearScreen()
		current, err := service.policyValues()
		draw := func() {
			ui.MenuTitle("系统工具", Title, "APT 策略与开关")
			if err != nil {
				ui.PrintWarningCard("读取生效策略失败", ui.CardField{Label: "原因", Value: err.Error()})
				fmt.Println()
			}
			for index, option := range policyOptions {
				if section := policySections[option.key]; section != "" {
					if index > 0 {
						fmt.Println()
					}
					ui.MenuSection(section)
				}
				status := ""
				if err == nil {
					status = option.display(current[option.key], current[intervalKey])
				}
				ui.MenuOptionHint(strconv.Itoa(index+1), option.label, status)
			}
			ui.MenuExit("0/q", "返回自动更新管理")
		}
		choice, err := ui.AskMenuChoice(view, "请选择", "", choices, draw)
		if err != nil {
			return err
		}
		index, _ := strconv.Atoi(choice)
		ui.RunAction(view, "修改 APT 策略失败", func() error {
			return service.editOption(view, policyOptions[index-1])
		})
	}
}

func (service manager) policyValues() (map[string]string, error) {
	args := []string{"shell", "PREFERRED", intervalKey}
	for index, option := range policyOptions {
		key := option.key
		if option.kind == "toggle" {
			key += "/b"
		}
		args = append(args, fmt.Sprintf("OPT%d", index), key)
	}
	output, err := service.query("apt-config", args...)
	if err != nil {
		return nil, err
	}
	assigned := assignments(output)
	current := map[string]string{intervalKey: assigned["PREFERRED"]}
	for index, option := range policyOptions {
		current[option.key] = assigned[fmt.Sprintf("OPT%d", index)]
	}
	return current, nil
}

func (option aptOption) display(value, preferred string) string {
	switch option.kind {
	case "frequency":
		if !activeInterval(value) && activeInterval(preferred) {
			return intervalLabel(value) + " · 已保存" + intervalLabel(preferred)
		}
		return intervalLabel(value)
	case "interval":
		return intervalLabel(value)
	}
	suffix := ""
	if value == "" && option.fallback != "" {
		value, suffix = option.fallback, "（默认）"
	}
	switch option.kind {
	case "toggle", "numeric toggle":
		return switchLabel(value) + suffix
	case "reboot time":
		if value == "now" {
			return "立即重启" + suffix
		}
		return value + suffix
	case "report":
		switch value {
		case "always":
			return "始终发送"
		case "on-change":
			return "有变更时发送"
		case "only-on-error":
			return "仅错误时发送"
		case "":
			return "使用默认策略"
		}
	}
	if value == "" {
		return "未设置"
	}
	return value
}

func (service manager) editOption(view *ui.UI, option aptOption) error {
	if !service.root() {
		return fmt.Errorf("修改 APT 策略需要 root 权限")
	}
	previous, exists, err := readManaged(service.path, managedHeader)
	if err != nil {
		return err
	}
	current, err := service.query("apt-config", "shell", "CURRENT", option.key)
	if err != nil {
		return err
	}
	before := assignments(current)["CURRENT"]
	value := before
	preferred := ""
	if option.kind == "frequency" && !activeInterval(value) {
		settings, err := service.settings()
		if err != nil {
			return err
		}
		value, preferred = settings.preferred, settings.preferred
	}
	if value == "" {
		value = option.fallback
	}
	showPage(option.label)
	ui.PrintInfoCard("当前设置",
		ui.CardField{Label: "当前值", Value: option.display(before, preferred)},
		ui.CardField{Label: "APT 参数", Value: option.key},
	)
	fmt.Println()
	value, err = askOption(view, option.kind, value)
	if err != nil {
		return err
	}
	if err := confirm(view, fmt.Sprintf("确认将%s设为 %q？(y/N)：", option.label, value)); err != nil {
		return err
	}
	options := map[string]string{option.key: value}
	if option.kind == "frequency" {
		options[intervalKey] = value
		if !activeInterval(before) {
			delete(options, upgradeKey)
		}
	}
	if option.key == shutdownKey {
		options[shutdownPreference] = value
	}
	return service.saveOptions(previous, exists, options)
}

func askOption(view *ui.UI, kind, current string) (string, error) {
	switch kind {
	case "toggle", "numeric toggle":
		if kind == "numeric toggle" {
			return ui.AskChoice(view, "输入 1 开启或 2 关闭", "", map[string]string{"1": "1", "2": "0"})
		}
		return ui.AskChoice(view, "输入 1 开启或 2 关闭", "", map[string]string{"1": "true", "2": "false"})
	case "report":
		return ui.AskChoice(view, "输入 1 始终发送、2 有变更时发送、3 仅错误时发送", "", map[string]string{
			"1": "always", "2": "on-change", "3": "only-on-error",
		})
	}
	prompt := "请输入天数或 always（每次触发）"
	back := "0/q 返回"
	switch kind {
	case "interval":
		prompt += "，0 关闭"
		back = "q 返回"
	case "reboot time":
		prompt = "请输入重启时间（HH:MM、+分钟或 now）"
	case "mail":
		prompt = "请输入报告邮箱地址，输入 - 清除"
	}
	value, err := view.Ask(prompt + "（回车沿用当前值，" + back + "）：")
	if err != nil {
		return "", err
	}
	if ui.IsReturnChoice(value) && !(value == "0" && kind == "interval") {
		return "", ui.ErrReturnToMenu
	}
	if value == "" {
		value = current
	}
	switch kind {
	case "interval", "frequency":
		if activeInterval(value) || kind == "interval" && value == "0" {
			return value, nil
		}
	case "reboot time":
		if value == "now" {
			return value, nil
		}
		if parsed, err := time.Parse("15:04", value); err == nil {
			return parsed.Format("15:04"), nil
		}
		if minutes, ok := strings.CutPrefix(value, "+"); ok {
			if delay, err := strconv.Atoi(minutes); err == nil && delay >= 0 {
				return "+" + strconv.Itoa(delay), nil
			}
		}
	case "mail":
		if value == "-" || value == "" {
			return "", nil
		}
		if address, err := mail.ParseAddress(value); err == nil && address.Address == value {
			return value, nil
		}
	}
	return "", fmt.Errorf("输入值无效")
}

func confirm(view *ui.UI, prompt string) error {
	accepted, err := view.Confirm(prompt)
	if err != nil {
		return err
	}
	if !accepted {
		return ui.ErrReturnToMenu
	}
	return nil
}
