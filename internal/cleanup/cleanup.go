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

	fmt.Printf("当前配置用户：%s\n", account.Name)
	fmt.Println()
	fmt.Println("请选择要清理的配置：")
	fmt.Println("1) 清理所有由本工具写入的配置")
	fmt.Println("2) 清理 SSH 公钥配置")
	fmt.Println("3) 清理 SSH 常用安全配置")
	fmt.Println("4) 清理 Vim 配置")
	fmt.Println("5) 清理 Bash 配置")
	fmt.Println("6) 清理 HTTP/HTTPS 代理配置")
	fmt.Println("0/q) 返回")
	fmt.Println()

	choice, err := view.Ask("输入选项: ")
	if err != nil {
		return err
	}
	fmt.Println()

	steps := allCleanupSteps()
	switch strings.ToLower(choice) {
	case "1":
		fmt.Println("警告：清理所有配置会移除本工具写入的 SSH 常用安全配置，可能恢复系统默认 SSH 密码登录。")
		confirmed, err := view.Confirm("确认清理所有由本工具写入的配置？请输入 y 确认，默认取消 (y/N): ")
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Println("已取消清理")
			return nil
		}
		return runCleanupSteps(account, steps)
	case "2":
		return runCleanupStepWithConfirm(view, account, steps[0])
	case "3":
		return runCleanupStepWithConfirm(view, account, steps[1])
	case "4":
		return runCleanupStepWithConfirm(view, account, steps[2])
	case "5":
		return runCleanupStepWithConfirm(view, account, steps[3])
	case "6":
		return runCleanupStepWithConfirm(view, account, steps[4])
	case "0", "q", "exit":
		return shared.ErrReturnToMenu
	default:
		fmt.Println("无效选项，已返回菜单")
		return nil
	}
}

func allCleanupSteps() []cleanupStep {
	return []cleanupStep{
		{name: "SSH 公钥", run: cleanupsshkeys.Run},
		{name: "SSH 常用安全", run: cleanupsshd.Run},
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
