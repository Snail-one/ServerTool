package cleanup

import (
	"errors"
	"fmt"
	"strings"

	cleanupbash "snail_tool/internal/cleanup/bash"
	cleanupproxy "snail_tool/internal/cleanup/proxy"
	cleanupsshd "snail_tool/internal/cleanup/sshd"
	cleanupsshkeys "snail_tool/internal/cleanup/sshkeys"
	cleanupvim "snail_tool/internal/cleanup/vim"
	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

type cleanupStep struct {
	name string
	run  func(*system.Account) error
}

func Run(view *ui.UI) error {
	account, err := system.CurrentTargetUser()
	if err != nil {
		return err
	}

	steps := allCleanupSteps()
	for {
		ui.ClearScreen()
		ui.MenuTitle("清理本工具配置")
		fmt.Printf("当前配置用户：%s\n", account.Name)
		fmt.Println()
		fmt.Println("1) 清理 SSH 公钥配置")
		fmt.Println("2) 清理 SSH 安全策略配置")
		fmt.Println("3) 清理 Vim 配置")
		fmt.Println("4) 清理 Bash 配置")
		fmt.Println("5) 清理 HTTP/HTTPS 代理配置")
		fmt.Println("6) 清理全部本工具配置")
		fmt.Println("0/q) 返回")
		fmt.Println()

		choice, err := view.Ask("输入选项: ")
		if err != nil {
			return err
		}
		fmt.Println()

		if shared.IsReturnChoice(choice) {
			return shared.ErrReturnToMenu
		}
		switch strings.ToLower(choice) {
		case "1":
			return runCleanupStepWithConfirm(view, account, steps[0])
		case "2":
			return runCleanupStepWithConfirm(view, account, steps[1])
		case "3":
			return runCleanupStepWithConfirm(view, account, steps[2])
		case "4":
			return runCleanupStepWithConfirm(view, account, steps[3])
		case "5":
			return runCleanupStepWithConfirm(view, account, steps[4])
		case "6":
			fmt.Println("警告：清理全部配置会移除本工具写入的 SSH 安全策略，可能恢复系统默认 SSH 密码登录。")
			confirmed, err := view.Confirm("确认清理全部本工具配置？请输入 y 确认，默认取消 (y/N): ")
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("已取消清理")
				view.Pause()
				continue
			}
			return runCleanupSteps(account, steps)
		default:
			fmt.Println("无效选项，请重新输入")
			view.Pause()
		}
	}
}

func allCleanupSteps() []cleanupStep {
	return []cleanupStep{
		{name: "SSH 公钥", run: cleanupsshkeys.Run},
		{name: "SSH 安全策略", run: cleanupsshd.Run},
		{name: "Vim", run: cleanupvim.Run},
		{name: "Bash", run: cleanupbash.Run},
		{name: "代理", run: cleanupproxy.Run},
	}
}

func runCleanupStepWithConfirm(view *ui.UI, account *system.Account, step cleanupStep) error {
	confirmed, err := view.Confirm(fmt.Sprintf("确认清理 %s 配置？(y/N): ", step.name))
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
	log.Info("清理完成")
	return nil
}
