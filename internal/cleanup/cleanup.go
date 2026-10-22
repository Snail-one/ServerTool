package cleanup

import (
	"errors"
	"fmt"
	"strconv"

	cleanupbackups "snail_tool/internal/cleanup/backups"
	cleanupbash "snail_tool/internal/cleanup/bash"
	cleanupproxy "snail_tool/internal/cleanup/proxy"
	cleanupsshd "snail_tool/internal/cleanup/sshd"
	cleanupsshkeys "snail_tool/internal/cleanup/sshkeys"
	cleanupvim "snail_tool/internal/cleanup/vim"
	commontmux "snail_tool/internal/common/tmux"
	commonups "snail_tool/internal/common/ups"
	goenvironment "snail_tool/internal/environment/golang"
	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/toolbox/firewall"
	"snail_tool/internal/ui"
)

type cleanupStep struct {
	name    string
	label   string
	run     func(*system.Account) error
	detail  string
	confirm func(*ui.UI) error
}

func Run(view *ui.UI) error {
	account, err := system.CurrentTargetUser()
	if err != nil {
		return err
	}

	steps := allCleanupSteps()
	backupChoice := strconv.Itoa(len(steps) + 1)
	allChoice := strconv.Itoa(len(steps) + 2)
	for {
		ui.ClearScreen()
		ui.MenuTitle("清理本工具配置")
		ui.PrintInfoCard("清理目标", ui.CardField{Label: "当前配置用户", Value: account.Name})
		fmt.Println()
		for index, step := range steps {
			ui.MenuOption(strconv.Itoa(index+1), "清理 "+step.label)
		}
		fmt.Println()
		ui.MenuOptionHint(backupChoice, "清理备份", "按模块 / 全部")
		ui.MenuOption(allChoice, "清理全部本工具配置")
		ui.MenuExit("0/q", "返回")
		fmt.Println()

		choice, err := view.Ask("请选择：")
		if err != nil {
			return err
		}
		fmt.Println()

		if shared.IsReturnChoice(choice) {
			return shared.ErrReturnToMenu
		}
		if key, err := strconv.Atoi(choice); err == nil && key >= 1 && key <= len(steps) {
			return runCleanupStepWithConfirm(view, account, steps[key-1])
		}
		switch choice {
		case allChoice:
			plan, err := cleanupbackups.PrepareAll()
			if err != nil {
				return err
			}
			ui.PrintDangerCard("清理全部配置风险",
				ui.CardField{Label: "SSH 安全策略", Value: "将移除本工具写入的配置"},
				ui.CardField{Label: "潜在影响", Value: "可能恢复系统默认 SSH 密码登录"},
				ui.CardField{Label: "tmux", Value: "移除目标用户的托管配置块；已有会话保持当前设置"},
				ui.CardField{Label: "防火墙", Value: "恢复首次快照中的配置和自启状态；无快照时移除托管区块；删除 inet snail 表，解除本工具限制"},
				ui.CardField{Label: "UPS", Value: "停用相关 NUT 服务，恢复首次备份或移除可识别的工具配置"},
				ui.CardField{Label: "Docker", Value: "移除日志轮转和服务代理，可能重启 Docker 并影响容器"},
				ui.CardField{Label: "Go 环境", Value: "移除托管 PATH 和命令链接，保留已安装版本"},
				ui.CardField{Label: "其他范围", Value: "软件、容器数据和旧位置备份使用独立清理入口"},
				ui.CardField{Label: "备份清理", Value: plan.Summary(), Detail: "永久删除全部模块备份，包含防火墙和 UPS 首次快照"},
			)
			confirmed, err := view.Confirm("确认清理全部本工具配置及备份？请输入 y 确认，默认取消 (y/N)：")
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("已取消清理")
				view.Pause()
				continue
			}
			return runAllCleanupSteps(account, steps, plan.Delete)
		case backupChoice:
			shared.RunAction(view, "备份清理失败，已返回清理配置", func() error { return cleanupbackups.Run(view) })
		default:
			ui.InvalidChoice()
			view.Pause()
		}
	}
}

func runAllCleanupSteps(account *system.Account, steps []cleanupStep, deleteBackups func() error) error {
	if err := runCleanupSteps(account, steps); err != nil {
		log.Warn("配置清理未全部完成，已保留备份")
		return err
	}
	return deleteBackups()
}

func allCleanupSteps() []cleanupStep {
	return []cleanupStep{
		{name: "SSH 公钥", label: "SSH 公钥配置", run: cleanupsshkeys.Run},
		{name: "SSH 安全策略", label: "SSH 安全策略配置", run: cleanupsshd.Run},
		{name: "Vim", label: "Vim 配置", run: cleanupvim.Run},
		{name: "Bash", label: "Bash 配置", run: cleanupbash.Run},
		{name: "tmux", label: "tmux 配置", run: commontmux.Cleanup, detail: "移除托管配置块；已有 tmux 会话保持当前设置"},
		{name: "代理", label: "HTTP/HTTPS 代理配置", run: cleanupproxy.Run},
		{name: "UPS", label: "UPS 配置", run: commonups.Cleanup, detail: "停用相关 NUT 服务，恢复首次备份或移除可识别的工具配置"},
		{name: "Docker", label: "Docker 配置", run: cleanupDocker, detail: "移除日志轮转和服务代理，可能重启 Docker 并影响容器"},
		{name: "Go 环境", label: "Go 环境配置", run: goenvironment.CleanupEnvironment, detail: "移除托管 PATH 和命令链接，保留已安装 Go 版本"},
		{name: "防火墙", label: "防火墙配置", run: firewall.Cleanup, confirm: firewall.CleanupWithConfirm, detail: "恢复首次快照中的完整配置和自启状态，覆盖接入后的文件修改；无快照时只移除托管区块；删除 inet snail 表，解除本工具限制"},
	}
}

func runCleanupStepWithConfirm(view *ui.UI, account *system.Account, step cleanupStep) error {
	if step.confirm != nil {
		return step.confirm(view)
	}
	if step.detail != "" {
		ui.PrintDangerCard("清理 "+step.label, ui.CardField{Label: "操作范围", Value: step.detail})
	}
	confirmed, err := view.Confirm(fmt.Sprintf("确认清理 %s？(y/N)：", step.label))
	if err != nil {
		return err
	}
	if !confirmed {
		fmt.Println("已取消清理")
		return nil
	}
	return runCleanupSteps(account, []cleanupStep{step})
}

func runCleanupSteps(account *system.Account, steps []cleanupStep) error {
	var errs []error
	for _, step := range steps {
		fmt.Println()
		log.Info("清理 ", step.name, " 配置...")
		if err := step.run(account); err != nil {
			log.Error(step.name, " 配置清理失败：", err)
			errs = append(errs, fmt.Errorf("%s: %w", step.name, err))
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	fmt.Println()
	fields := make([]ui.CardField, 0, len(steps))
	for _, step := range steps {
		fields = append(fields, ui.CardField{Label: step.name, Value: ui.Badge("已清理", true)})
	}
	ui.PrintSuccessCard("配置清理完成", fields...)
	return nil
}
