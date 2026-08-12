package quicksetup

import (
	"fmt"

	commonbash "snail_tool/internal/common/bash"
	commonvim "snail_tool/internal/common/vim"
	"snail_tool/internal/log"
	"snail_tool/internal/ssh/keys"
	"snail_tool/internal/ssh/security"
	"snail_tool/internal/ui"
)

type setupStep struct {
	name string
	run  func() error
}

func Run(view *ui.UI) error {
	ui.ClearScreen()
	ui.MenuTitle("一键配置")
	fmt.Println("将按顺序配置：SSH 公钥 → SSH 安全策略 → Vim → Bash")
	fmt.Println("未检测到 SSH 公钥时必须先添加，成功后才会配置 SSH 安全策略。")
	fmt.Println()

	steps := []setupStep{
		{name: "SSH 公钥", run: func() error { return keys.EnsureSSHAuthorizedKeys(view) }},
		{name: "SSH 安全策略", run: func() error { return security.ConfigureSSHSecurity(view) }},
		{name: "Vim 配置", run: func() error { return commonvim.ConfigureVim(view) }},
		{name: "Bash 配置", run: commonbash.ConfigureBash},
	}
	if err := runSetupSteps(steps); err != nil {
		return err
	}

	fmt.Println()
	log.Info("一键配置完成")
	fmt.Println("Bash 配置立即生效请执行：source ~/.bashrc")
	return nil
}

func runSetupSteps(steps []setupStep) error {
	for index, step := range steps {
		fmt.Printf("[%d/%d] %s\n", index+1, len(steps), step.name)
		if err := step.run(); err != nil {
			return fmt.Errorf("%s失败: %w", step.name, err)
		}
		fmt.Println()
		log.Info(step.name, "完成")
		fmt.Println()
	}
	return nil
}
