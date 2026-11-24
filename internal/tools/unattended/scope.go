package unattended

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"snail_tool/internal/ui"
)

const (
	scopeFile     = "99-zz-servertool-update-scope.conf"
	originsKey    = "Unattended-Upgrade::Origins-Pattern"
	allowedKey    = "Unattended-Upgrade::Allowed-Origins"
	blacklistKey  = "Unattended-Upgrade::Package-Blacklist"
	kernelPattern = "^linux-(image|headers|modules|kbuild|source|tools)(-|$)"
	dockerPattern = "^(docker[.]io|docker-ce(-cli|-rootless-extras)?|docker-cli|docker-buildx(-plugin)?|docker-compose(-plugin)?|containerd([.]io)?|runc)$"
)

type updateScope struct {
	Managed                                      bool
	Security, Ordinary, Kernel, Docker, Reminder bool
}

type scopeOption struct {
	label   string
	enabled func(*updateScope) *bool
}

var scopeOptions = map[string]scopeOption{
	"1": {"系统安全更新", func(s *updateScope) *bool { return &s.Security }},
	"2": {"系统普通更新", func(s *updateScope) *bool { return &s.Ordinary }},
	"3": {"Linux 内核更新", func(s *updateScope) *bool { return &s.Kernel }},
	"5": {"Docker Engine 更新", func(s *updateScope) *bool { return &s.Docker }},
	"6": {"Debian 大版本升级提醒", func(s *updateScope) *bool { return &s.Reminder }},
}

func (scope updateScope) origins() []string {
	var patterns []string
	if scope.Security {
		patterns = append(patterns,
			"origin=Debian,codename=${distro_codename},label=Debian-Security",
			"origin=Debian,codename=${distro_codename}-security,label=Debian-Security")
	}
	if scope.Ordinary {
		patterns = append(patterns,
			"origin=Debian,codename=${distro_codename},label=Debian",
			"origin=Debian,codename=${distro_codename}-updates,label=Debian")
	}
	if scope.Docker {
		patterns = append(patterns, "site=download.docker.com,codename=${distro_codename},component=stable")
	}
	return patterns
}

func (scope updateScope) exclusions() []string {
	var patterns []string
	if !scope.Kernel {
		patterns = append(patterns, kernelPattern)
	}
	if !scope.Docker {
		patterns = append(patterns, dockerPattern)
	}
	return patterns
}

func (scope updateScope) render() string {
	if !scope.Managed && !scope.Reminder {
		return ""
	}
	encoded, _ := json.Marshal(scope)
	var content strings.Builder
	fmt.Fprintf(&content, "%s// %s\n", managedHeader, encoded)
	if !scope.Managed {
		return content.String()
	}
	fmt.Fprintf(&content, "#clear %s;\n#clear %s;\n", allowedKey, originsKey)
	for _, origin := range scope.origins() {
		fmt.Fprintf(&content, "%s:: %q;\n", originsKey, origin)
	}
	for _, pattern := range scope.exclusions() {
		fmt.Fprintf(&content, "%s:: %q;\n", blacklistKey, pattern)
	}
	return content.String()
}

func (service manager) scopePath() string {
	return filepath.Join(filepath.Dir(service.path), scopeFile)
}

func (service manager) readScope() (scope updateScope, previous []byte, exists bool, err error) {
	path := service.scopePath()
	previous, exists, err = readManaged(path, managedHeader)
	if err != nil || !exists {
		return
	}
	header, _, _ := bytes.Cut(bytes.TrimPrefix(previous, []byte(managedHeader+"// ")), []byte("\n"))
	if json.Unmarshal(header, &scope) != nil || scope.render() != string(previous) {
		err = fmt.Errorf("更新范围配置有外部修改，请检查 %s", path)
	}
	return
}

func (service manager) saveScope(scope updateScope, previous []byte, exists bool) error {
	return publish(service.scopePath(), previous, exists, scope.render(), func() error {
		output, err := service.query("apt-config", "dump")
		if err != nil || !scope.Managed {
			return err
		}
		config := aptScopeLists(output)
		expected := scope.origins()
		if !samePatterns(config[originsKey], expected) || len(config[allowedKey]) != 0 {
			return fmt.Errorf("生效的 APT 更新来源与所选范围不一致")
		}
		for _, pattern := range scope.exclusions() {
			if config[blacklistKey][pattern] == 0 {
				return fmt.Errorf("生效的 APT 配置缺少指定的软件包排除规则")
			}
		}
		return nil
	})
}

func aptScopeLists(output string) map[string]map[string]int {
	lists := map[string]map[string]int{originsKey: {}, allowedKey: {}, blacklistKey: {}}
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(line, " ")
		value = strings.TrimSpace(value)
		if !ok || len(value) <= 3 || !strings.HasPrefix(value, `"`) || !strings.HasSuffix(value, `";`) {
			continue
		}
		for name, items := range lists {
			// apt-config dump prints list entries with the root and a trailing ::.
			if strings.EqualFold(key, name) || strings.HasPrefix(strings.ToLower(key), strings.ToLower(name+"::")) {
				items[value[1:len(value)-2]]++
			}
		}
	}
	return lists
}

func samePatterns(actual map[string]int, expected []string) bool {
	if len(actual) != len(expected) {
		return false
	}
	for _, item := range expected {
		if actual[item] != 1 {
			return false
		}
	}
	return true
}

func scopeLabel(enabled bool) string {
	if enabled {
		return "开启"
	}
	return "关闭"
}

func (scope updateScope) optionLabel(key string) string {
	if key != "6" && !scope.Managed {
		return "沿用原生策略"
	}
	return scopeLabel(*scopeOptions[key].enabled(&scope))
}

func (service manager) scopeMenu(view *ui.UI) error {
	for {
		ui.ClearScreen()
		scope, previous, exists, err := service.readScope()
		if err != nil {
			return err
		}
		policy, err := service.policyValues()
		if err != nil {
			return err
		}
		choices := map[string]string{"1": "1", "2": "2", "3": "3", "4": "4", "5": "5", "6": "6", "7": "7"}
		if scope.Managed {
			choices["x"] = "x"
		}
		draw := func() {
			ui.MenuTitle("系统工具", Title, "更新范围与开关")
			value := "沿用系统生效的 APT 更新来源"
			if scope.Managed {
				value = "按当前 Debian 版本及所选来源限制自动更新"
			}
			fields := []ui.CardField{{Label: "更新来源", Value: value}}
			for _, key := range []string{"1", "2", "3"} {
				fields = append(fields, ui.CardField{Label: scopeOptions[key].label, Value: scope.optionLabel(key)})
			}
			cleanup := ui.CardField{Label: "旧内核清理", Value: oldKernelOption.display(policy[oldKernelOption.key], "")}
			if cleanup.Value == "关闭" {
				cleanup.Detail = "其他自动依赖清理也可能移除闲置内核"
			}
			fields = append(fields,
				cleanup,
				ui.CardField{Label: scopeOptions["5"].label, Value: scope.optionLabel("5")},
				ui.CardField{Label: scopeOptions["6"].label, Value: scope.optionLabel("6")},
				ui.CardField{Label: "服务器自动重启", Value: rebootOption.display(policy[rebootOption.key], "")},
			)
			ui.PrintInfoCard("当前策略", fields...)
			fmt.Println()
			ui.MenuOption("1", scopeOptions["1"].label)
			ui.MenuOption("2", scopeOptions["2"].label)
			ui.MenuOptionHint("3", scopeOptions["3"].label, "从已允许的来源选择内核更新")
			ui.MenuOption("4", "旧内核清理")
			ui.MenuOptionHint("5", scopeOptions["5"].label, "包含已配置的 Docker 官方稳定仓库")
			ui.MenuOptionHint("6", scopeOptions["6"].label, "检查 Debian 稳定版是否有新大版本")
			ui.MenuOption("7", "服务器自动重启")
			if scope.Managed {
				fmt.Println()
				ui.MenuShortcutHint("x", "恢复原生更新范围", "移除本工具设定的来源和排除规则")
			}
			ui.MenuExit("0/q", "返回自动更新管理")
		}
		choice, err := ui.AskMenuChoice(view, "请选择", "", choices, draw)
		if err != nil {
			return err
		}
		ui.RunAction(view, "修改更新策略失败", func() error {
			switch choice {
			case "4":
				return service.editOption(view, oldKernelOption)
			case "7":
				return service.editOption(view, rebootOption)
			default:
				return service.editScope(view, choice, scope, previous, exists)
			}
		})
	}
}

func (service manager) editScope(view *ui.UI, choice string, scope updateScope, previous []byte, exists bool) error {
	if !service.root() {
		return fmt.Errorf("修改更新策略需要 root 权限")
	}
	if choice == "x" {
		if !scope.Managed {
			return ui.ErrReturnToMenu
		}
		if err := confirm(view, "确认恢复原生更新范围？(y/N)："); err != nil {
			return err
		}
		return service.saveScope(updateScope{Reminder: scope.Reminder}, previous, exists)
	}
	if _, err := localDebianMajor(); err != nil {
		return err
	}
	option := scopeOptions[choice]
	showPage(option.label)
	ui.PrintInfoCard("当前设置", ui.CardField{Label: option.label, Value: scope.optionLabel(choice)})
	fmt.Println()
	value, err := ui.AskChoice(view, "输入 1 开启或 2 关闭", "", map[string]string{"1": "on", "2": "off"})
	if err != nil {
		return err
	}
	if choice != "6" && !scope.Managed {
		scope = updateScope{Managed: true, Security: true, Kernel: true, Reminder: scope.Reminder}
	}
	*option.enabled(&scope) = value == "on"
	prompt := fmt.Sprintf("确认%s%s？(y/N)：", scopeLabel(value == "on"), option.label)
	if scope.Managed && choice != "6" {
		fields := []ui.CardField{}
		for _, key := range []string{"1", "2", "3", "5"} {
			fields = append(fields, ui.CardField{Label: scopeOptions[key].label, Value: scope.optionLabel(key)})
		}
		ui.PrintInfoCard("将应用的更新范围", fields...)
		prompt = "确认应用以上更新范围？(y/N)："
	}
	if err := confirm(view, prompt); err != nil {
		return err
	}
	return service.saveScope(scope, previous, exists)
}
