package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

type cleanupStep struct {
	name string
	run  func(*system.Account) error
}

const (
	legacyBashCommandBegin = "# ===== BEGIN SNAIL COMMAND ====="
	legacyBashCommandEnd   = "# ===== END SNAIL COMMAND ====="
)

func CleanupConfig(view *ui.UI) error {
	account, err := system.CurrentTargetUser()
	if err != nil {
		return err
	}

	fmt.Printf("当前配置用户：%s\n", account.Name)
	fmt.Println()
	fmt.Println("请选择要清理的配置：")
	fmt.Println("1) 清理所有由本工具写入的配置")
	fmt.Println("2) 清理 SSH 配置")
	fmt.Println("3) 清理 Vim 配置")
	fmt.Println("4) 清理 Bash 配置")
	fmt.Println("5) 清理 HTTP/HTTPS 代理配置")
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
		confirmed, err := view.Confirm("确认清理所有由本工具写入的配置？(y/N): ")
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
	case "0", "q", "exit":
		return nil
	default:
		fmt.Println("无效选项，已返回菜单")
		return nil
	}
}

func allCleanupSteps() []cleanupStep {
	return []cleanupStep{
		{name: "SSH", run: cleanupSSHConfig},
		{name: "Vim", run: cleanupVimConfig},
		{name: "Bash", run: cleanupBashConfig},
		{name: "代理", run: cleanupProxyConfig},
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

func cleanupSSHConfig(account *system.Account) error {
	var errs []error
	if err := cleanupSSHAuthorizedKeys(account); err != nil {
		errs = append(errs, err)
	}

	sshServiceChanged, err := cleanupSSHDConfig()
	if err != nil {
		errs = append(errs, err)
	}
	if sshServiceChanged {
		if err := reloadSSHService(); err != nil {
			errs = append(errs, fmt.Errorf("重新加载 SSH 服务失败: %w", err))
		}
	}

	return errors.Join(errs...)
}

func cleanupSSHAuthorizedKeys(account *system.Account) error {
	authKeys := filepath.Join(account.Home, ".ssh", "authorized_keys")
	if !system.FileExists(authKeys) {
		log.Info("未发现 SSH authorized_keys，跳过")
		return nil
	}

	data, err := os.ReadFile(authKeys)
	if err != nil {
		return err
	}

	content := string(data)
	cleaned := removeManagedBlock(content, sshAuthorizedKeysBegin, sshAuthorizedKeysEnd)
	if cleaned == content {
		log.Warn("未发现由本工具标记的 SSH 公钥，已保留 authorized_keys")
		return nil
	}

	cleaned = normalizeCleanedContent(cleaned)
	if err := os.WriteFile(authKeys, []byte(cleaned), 0600); err != nil {
		return err
	}
	if err := os.Chmod(authKeys, 0600); err != nil {
		return err
	}

	log.Info("已清理本工具写入的 SSH 公钥块")
	return nil
}

func cleanupSSHDConfig() (bool, error) {
	serviceChanged := false

	includeChanged, err := cleanupSSHDInclude()
	if err != nil {
		return serviceChanged, err
	}
	serviceChanged = serviceChanged || includeChanged

	if !system.FileExists(customSSHDConfigPath) {
		log.Info("未发现 SSH 自定义配置，跳过")
		return serviceChanged, nil
	}

	data, err := os.ReadFile(customSSHDConfigPath)
	if err != nil {
		return serviceChanged, err
	}
	if !strings.Contains(string(data), managedSSHDConfigHeader) {
		log.Warn("SSH 自定义配置不是本工具生成，已跳过：", customSSHDConfigPath)
		return serviceChanged, nil
	}

	if err := os.WriteFile(customSSHDConfigPath, nil, 0644); err != nil {
		return serviceChanged, err
	}
	log.Info("已清空 SSH 自定义配置：", customSSHDConfigPath)
	return true, nil
}

func cleanupSSHDInclude() (bool, error) {
	if !system.FileExists(sshdConfigPath) {
		return false, nil
	}

	data, err := os.ReadFile(sshdConfigPath)
	if err != nil {
		return false, err
	}

	content := string(data)
	cleaned := removeManagedBlock(content, managedSSHDIncludeBegin, managedSSHDIncludeEnd)
	if cleaned == content {
		return false, nil
	}
	cleaned = normalizeCleanedContent(cleaned)

	if err := os.WriteFile(sshdConfigPath, []byte(cleaned), 0644); err != nil {
		return false, err
	}
	log.Info("已清理本工具写入的 SSH Include 配置")
	return true, nil
}

func cleanupVimConfig(account *system.Account) error {
	vimrc := filepath.Join(account.Home, ".vimrc")
	if !system.FileExists(vimrc) {
		log.Info("未发现 Vim 配置，跳过")
		return nil
	}

	content := readFileString(vimrc)
	if strings.TrimSpace(content) != strings.TrimSpace(vimrcContent) {
		log.Warn("当前 Vim 配置与本工具模板不完全一致，已跳过：", vimrc)
		return nil
	}

	if err := os.WriteFile(vimrc, nil, 0644); err != nil {
		return err
	}
	log.Info("已清空 Vim 配置：", vimrc)
	return nil
}

func cleanupBashConfig(account *system.Account) error {
	bashrc := filepath.Join(account.Home, ".bashrc")
	result, err := cleanupManagedBlocks(
		bashrc,
		blockMarker{begin: bashAliasBegin, end: bashAliasEnd},
		blockMarker{begin: legacyBashCommandBegin, end: legacyBashCommandEnd},
	)
	if err != nil {
		return err
	}
	if result.changed {
		log.Info("已清理 Bash 托管配置：", bashrc)
	} else {
		log.Info("未发现 Bash 托管配置，跳过")
	}
	return nil
}

func cleanupProxyConfig(account *system.Account) error {
	bashrc := filepath.Join(account.Home, ".bashrc")
	result, err := cleanupManagedBlocks(
		bashrc,
		blockMarker{begin: proxyBegin, end: proxyEnd},
	)
	if err != nil {
		return err
	}
	for _, name := range proxyCleanupEnvNames() {
		_ = os.Unsetenv(name)
	}
	if result.changed {
		log.Info("已清理代理托管配置：", bashrc)
		fmt.Println("当前终端已存在的代理环境变量可能需要重新登录或手动 unset 后才会消失。")
	} else {
		log.Info("未发现代理托管配置，跳过")
	}
	return nil
}

type blockMarker struct {
	begin string
	end   string
}

type managedBlockCleanupResult struct {
	changed bool
}

func cleanupManagedBlocks(path string, blocks ...blockMarker) (managedBlockCleanupResult, error) {
	if !system.FileExists(path) {
		return managedBlockCleanupResult{}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return managedBlockCleanupResult{}, err
	}

	content := string(data)
	cleaned := content
	for _, block := range blocks {
		cleaned = removeManagedBlock(cleaned, block.begin, block.end)
	}
	if cleaned == content {
		return managedBlockCleanupResult{}, nil
	}

	cleaned = normalizeCleanedContent(cleaned)
	return managedBlockCleanupResult{changed: true}, os.WriteFile(path, []byte(cleaned), 0644)
}

func normalizeCleanedContent(content string) string {
	if strings.TrimSpace(content) == "" {
		return ""
	}
	return strings.Trim(content, "\n") + "\n"
}

func proxyCleanupEnvNames() []string {
	names := append([]string{}, proxyEnvNames...)
	return append(names, "NO_PROXY", "no_proxy")
}
