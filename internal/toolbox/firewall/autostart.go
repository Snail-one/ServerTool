package firewall

import (
	"fmt"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

const nftablesUnit = "nftables.service"

type autostartStatus struct {
	label     string
	state     string
	enabled   bool
	execStart string
	err       error
}

func inspectAutostart() autostartStatus {
	if !commandExists("systemctl") {
		return autostartStatus{label: "不支持", err: fmt.Errorf("当前系统没有 systemctl，开机自启开关仅支持 systemd")}
	}
	out, err := commandOutput("systemctl", "show", nftablesUnit, "--property=LoadState", "--property=UnitFileState", "--property=ExecStart")
	if err != nil {
		return autostartStatus{label: "检测失败", err: fmt.Errorf("读取 nftables 开机自启状态失败: %w: %s", err, strings.TrimSpace(out))}
	}
	properties := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			properties[key] = value
		}
	}
	status := autostartStatus{state: properties["UnitFileState"], execStart: properties["ExecStart"]}
	if properties["LoadState"] != "loaded" {
		status.label = "服务不可用"
		status.err = fmt.Errorf("%s 不可用（%s），请检查 nftables 服务安装或屏蔽状态", nftablesUnit, properties["LoadState"])
		return status
	}
	switch status.state {
	case "enabled":
		status.label, status.enabled = "已开启", true
	case "disabled", "linked", "linked-runtime":
		status.label = "已关闭"
	case "enabled-runtime":
		status.label = "临时开启，重启后失效"
	default:
		status.label = "不可切换"
		status.err = fmt.Errorf("%s 当前状态为 %q，无法直接切换开机自启", nftablesUnit, status.state)
	}
	return status
}

// The distribution's service must actually load our config path. Some systems
// ship a service that reads /etc/sysconfig/nftables.conf instead.
func serviceLoadsConfig(execStart string) bool {
	_, arguments, ok := strings.Cut(execStart, "argv[]=")
	if !ok {
		return false
	}
	arguments, _, _ = strings.Cut(arguments, ";")
	fields := strings.Fields(arguments)
	for i := 1; i < len(fields); i++ {
		if fields[i] == "--file="+configPath {
			return true
		}
		if i+1 < len(fields) && (fields[i] == "-f" || fields[i] == "--file") && fields[i+1] == configPath {
			return true
		}
	}
	return false
}

func toggleAutostart(view *ui.UI) error {
	if !isRoot() {
		return fmt.Errorf("修改防火墙开机自启需要 root 权限，请使用 sudo 运行本工具")
	}
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "开机自启设置")
	status := inspectAutostart()
	if status.err != nil {
		return status.err
	}
	action, target := "enable", "开启"
	var version configVersion
	if status.enabled {
		action, target = "disable", "关闭"
	} else {
		if !serviceLoadsConfig(status.execStart) {
			return fmt.Errorf("无法确认 %s 启动时加载 %s，未开启；请先检查该服务的 ExecStart 配置", nftablesUnit, configPath)
		}
		var err error
		version, err = readConfigVersion()
		if err != nil {
			return err
		}
		if !version.exists || len(strings.TrimSpace(string(version.content))) == 0 {
			return fmt.Errorf("未找到有效的 %s，请先生成防火墙配置", configPath)
		}
		tokens, err := configTokens(string(version.content))
		if err != nil || len(tokens) == 0 {
			return fmt.Errorf("%s 为空、仅含注释或语法不完整，请先生成有效防火墙配置", configPath)
		}
		if out, err := commandOutput("nft", "-c", "-f", configPath); err != nil {
			return fmt.Errorf("防火墙配置校验失败，未开启开机自启: %w: %s", err, strings.TrimSpace(out))
		}
	}
	ui.PrintInfoCard("防火墙开机自启",
		ui.CardField{Label: "当前状态", Value: status.label},
		ui.CardField{Label: "操作", Value: target + "开机自启"},
		ui.CardField{Label: "影响", Value: "仅修改开机自启，当前已加载规则保持生效"},
	)
	confirmed, err := view.Confirm("是否" + target + "防火墙开机自启？(y/N)：")
	if err != nil {
		return err
	}
	if !confirmed {
		return shared.ErrReturnToMenu
	}
	latest := inspectAutostart()
	if latest.err != nil {
		return latest.err
	}
	if latest.state != status.state || latest.execStart != status.execStart {
		return fmt.Errorf("nftables 服务状态或启动命令已变化，未修改，请重新操作")
	}
	if action == "enable" {
		if err := checkConfigVersion(version); err != nil {
			return err
		}
	}
	if err := preserveOriginal(); err != nil {
		return err
	}
	// Do not use --now: stopping nftables may run ExecStop=flush ruleset,
	// affecting all tables, and starting it would load pending config edits.
	if err := commandRun("systemctl", action, nftablesUnit); err != nil {
		return fmt.Errorf("%s防火墙开机自启失败: %w", target, err)
	}
	result := inspectAutostart()
	if result.err != nil {
		return fmt.Errorf("命令已执行，但无法确认开机自启状态: %w", result.err)
	}
	if result.enabled != (action == "enable") {
		return fmt.Errorf("命令已执行，但开机自启未达到预期状态，当前为 %s", result.label)
	}
	log.Info("防火墙开机自启已", target, "，当前已加载规则保持生效")
	return nil
}
