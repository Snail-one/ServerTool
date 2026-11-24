package autostart

import (
	"fmt"
	"strings"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/log"
	"snail_tool/internal/ui"
)

const Unit = "nftables.service"

type Service struct {
	Store         configuration.Store
	IsRoot        func() bool
	CommandExists func(string) bool
	CommandRun    func(string, ...string) error
}

type Status struct {
	Label     string
	State     string
	Enabled   bool
	ExecStart string
	Err       error
}

func (service Service) Inspect() Status {
	if !service.CommandExists("systemctl") {
		return Status{Label: "不支持", Err: fmt.Errorf("当前系统没有 systemctl，开机自启开关仅支持 systemd")}
	}
	out, err := service.Store.CommandOutput("systemctl", "show", Unit, "--property=LoadState", "--property=UnitFileState", "--property=ExecStart")
	if err != nil {
		return Status{Label: "检测失败", Err: fmt.Errorf("读取 nftables 开机自启状态失败: %w: %s", err, strings.TrimSpace(out))}
	}
	properties := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			properties[key] = value
		}
	}
	status := Status{State: properties["UnitFileState"], ExecStart: properties["ExecStart"]}
	if properties["LoadState"] != "loaded" {
		status.Label = "服务不可用"
		status.Err = fmt.Errorf("%s 不可用（%s），请检查 nftables 服务安装或屏蔽状态", Unit, properties["LoadState"])
		return status
	}
	switch status.State {
	case "enabled":
		status.Label, status.Enabled = "已开启", true
	case "disabled", "linked", "linked-runtime":
		status.Label = "已关闭"
	case "enabled-runtime":
		status.Label = "临时开启，重启后失效"
	default:
		status.Label = "不可切换"
		status.Err = fmt.Errorf("%s 当前状态为 %q，无法直接切换开机自启", Unit, status.State)
	}
	return status
}

// The distribution's service must actually load our config path. Some systems
// ship a service that reads /etc/sysconfig/nftables.conf instead.
func (service Service) LoadsConfig(execStart string) bool {
	_, arguments, ok := strings.Cut(execStart, "argv[]=")
	if !ok {
		return false
	}
	arguments, _, _ = strings.Cut(arguments, ";")
	fields := strings.Fields(arguments)
	for i := 1; i < len(fields); i++ {
		if fields[i] == "--file="+service.Store.Path {
			return true
		}
		if i+1 < len(fields) && (fields[i] == "-f" || fields[i] == "--file") && fields[i+1] == service.Store.Path {
			return true
		}
	}
	return false
}

// First configuration may enable boot loading after the rules are saved and
// applied. Rebuilds do not use this path and keep the user's startup setting.
func (service Service) PrepareFirst() (*Status, error) {
	if !service.CommandExists("systemctl") {
		return nil, nil
	}
	status := service.Inspect()
	if status.Err != nil {
		return nil, status.Err
	}
	if !service.LoadsConfig(status.ExecStart) {
		return nil, fmt.Errorf("无法确认 %s 启动时加载 %s，未生成配置；请先检查该服务的 ExecStart 配置", Unit, service.Store.Path)
	}
	return &status, nil
}

func (service Service) Check(expected Status) error {
	latest := service.Inspect()
	if latest.Err != nil {
		return latest.Err
	}
	if latest.State != expected.State || latest.ExecStart != expected.ExecStart {
		return fmt.Errorf("nftables 服务状态或启动命令已变化，未修改，请重新操作")
	}
	return nil
}

func (service Service) EnableFirst(expected Status, content []byte) error {
	if err := service.Check(expected); err != nil {
		return err
	}
	if err := service.Store.Check(configuration.Version{Content: content, Exists: true}); err != nil {
		return err
	}
	if expected.Enabled {
		return nil
	}
	if err := service.Store.PreserveOriginal(); err != nil {
		return err
	}
	// Loading already succeeded; enable without restarting or flushing any table.
	if err := service.CommandRun("systemctl", "enable", Unit); err != nil {
		return fmt.Errorf("开启防火墙开机自启失败: %w", err)
	}
	result := service.Inspect()
	if result.Err != nil {
		return fmt.Errorf("无法确认开机自启状态: %w", result.Err)
	}
	if !result.Enabled || result.ExecStart != expected.ExecStart {
		return fmt.Errorf("开机自启未达到预期状态或启动命令已变化，当前为 %s", result.Label)
	}
	return nil
}

func (service Service) Toggle(view *ui.UI) error {
	if !service.IsRoot() {
		return fmt.Errorf("修改防火墙开机自启需要 root 权限，请使用 sudo 运行本工具")
	}
	Page()
	status := service.Inspect()
	if status.Err != nil {
		return status.Err
	}
	action, target := "enable", "开启"
	var version configuration.Version
	if status.Enabled {
		action, target = "disable", "关闭"
	} else {
		if !service.LoadsConfig(status.ExecStart) {
			return fmt.Errorf("无法确认 %s 启动时加载 %s，未开启；请先检查该服务的 ExecStart 配置", Unit, service.Store.Path)
		}
		var err error
		version, err = service.Store.Read()
		if err != nil {
			return err
		}
		if !version.Exists || len(strings.TrimSpace(string(version.Content))) == 0 {
			return fmt.Errorf("未找到有效的 %s，请先生成防火墙配置", service.Store.Path)
		}
		tokens, err := configuration.Tokens(string(version.Content))
		if err != nil || len(tokens) == 0 {
			return fmt.Errorf("%s 为空、仅含注释或语法不完整，请先生成有效防火墙配置", service.Store.Path)
		}
		if out, err := service.Store.CommandOutput("nft", "-c", "-f", service.Store.Path); err != nil {
			return fmt.Errorf("防火墙配置校验失败，未开启开机自启: %w: %s", err, strings.TrimSpace(out))
		}
	}
	confirmed, err := Confirm(view, status.Label, target)
	if err != nil {
		return err
	}
	if !confirmed {
		return ui.ErrReturnToMenu
	}
	if err := service.Check(status); err != nil {
		return err
	}
	if action == "enable" {
		if err := service.Store.Check(version); err != nil {
			return err
		}
	}
	if err := service.Store.PreserveOriginal(); err != nil {
		return err
	}
	// Do not use --now: stopping nftables may run ExecStop=flush ruleset,
	// affecting all tables, and starting it would load pending config edits.
	if err := service.CommandRun("systemctl", action, Unit); err != nil {
		return fmt.Errorf("%s防火墙开机自启失败: %w", target, err)
	}
	result := service.Inspect()
	if result.Err != nil {
		return fmt.Errorf("命令已执行，但无法确认开机自启状态: %w", result.Err)
	}
	if result.Enabled != (action == "enable") {
		return fmt.Errorf("命令已执行，但开机自启未达到预期状态，当前为 %s", result.Label)
	}
	log.Info("防火墙开机自启已", target, "，当前已加载规则保持生效")
	return nil
}

// Never stop/restart nftables or use --now: ExecStop may flush unrelated tables.
func (service Service) Restore(target, current string) error {
	if current == "enabled-runtime" {
		if err := service.CommandRun("systemctl", "disable", "--runtime", Unit); err != nil {
			return err
		}
	}
	if err := service.CommandRun("systemctl", "disable", Unit); err != nil {
		return err
	}
	if target != "disabled" {
		args := []string{"enable", Unit}
		if target == "enabled-runtime" {
			args = []string{"enable", "--runtime", Unit}
		}
		if err := service.CommandRun("systemctl", args...); err != nil {
			return err
		}
	}
	status := service.Inspect()
	if status.Err != nil {
		return status.Err
	}
	if status.State != target {
		return fmt.Errorf("自启状态未恢复，当前 %q，预期 %q", status.State, target)
	}
	return nil
}
