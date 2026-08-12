package quicksetup

import (
	"fmt"
	"path/filepath"

	commonbash "snail_tool/internal/common/bash"
	commonvim "snail_tool/internal/common/vim"
	"snail_tool/internal/log"
	"snail_tool/internal/ssh/keys"
	"snail_tool/internal/ssh/security"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

type setupStep struct {
	name string
	run  func() error
}

type setupSummary struct {
	user              string
	sshKeys           bool
	sshKeysPath       string
	sshSecurity       bool
	sshConfigSource   string
	sshPort           string
	sshCommand        string
	vimConfigured     bool
	vimConfigPath     string
	bashConfigured    bool
	bashConfigPath    string
	bashSourceCommand string
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

	summary, err := loadSetupSummary()
	if err != nil {
		return fmt.Errorf("读取一键配置结果失败: %w", err)
	}
	printSetupSummaryCard(summary)
	return nil
}

func loadSetupSummary() (setupSummary, error) {
	account, err := system.CurrentTargetUser()
	if err != nil {
		return setupSummary{}, err
	}
	effectiveSSH, err := security.LoadEffectiveConfig()
	if err != nil {
		return setupSummary{}, err
	}

	sshKeysPath := filepath.Join(account.Home, ".ssh", "authorized_keys")
	vimConfigPath := filepath.Join(account.Home, ".vimrc")
	bashConfigPath := filepath.Join(account.Home, ".bashrc")
	return setupSummary{
		user:              account.Name,
		sshKeys:           keys.IsConfigured(account),
		sshKeysPath:       sshKeysPath,
		sshSecurity:       security.IsConfigured(),
		sshConfigSource:   effectiveSSH.Source,
		sshPort:           effectiveSSH.Port,
		sshCommand:        fmt.Sprintf("ssh -p %s %s@服务器IP", effectiveSSH.Port, account.Name),
		vimConfigured:     commonvim.IsVimConfigured(account),
		vimConfigPath:     vimConfigPath,
		bashConfigured:    commonbash.IsBashConfigured(account),
		bashConfigPath:    bashConfigPath,
		bashSourceCommand: "source " + bashConfigPath,
	}, nil
}

func printSetupSummaryCard(summary setupSummary) {
	fmt.Println()
	ui.PrintSuccessCard("一键配置完成",
		ui.CardField{Label: "用户", Value: summary.user},
		ui.CardField{Label: "SSH 公钥", Value: ui.ConfiguredBadge(summary.sshKeys), Detail: summary.sshKeysPath},
		ui.CardField{Label: "SSH 安全策略", Value: ui.ConfiguredBadge(summary.sshSecurity)},
		ui.CardField{Label: "SSH 配置", Value: summary.sshConfigSource},
		ui.CardField{Label: "SSH 端口", Value: summary.sshPort},
		ui.CardField{Label: "SSH 登录", Value: summary.sshCommand},
		ui.CardField{Label: "Vim 配置", Value: ui.ConfiguredBadge(summary.vimConfigured), Detail: summary.vimConfigPath},
		ui.CardField{Label: "Bash 配置", Value: ui.ConfiguredBadge(summary.bashConfigured), Detail: summary.bashConfigPath},
		ui.CardField{Label: "Bash 生效", Value: summary.bashSourceCommand},
	)
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
