package sshd

import (
	"fmt"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/ssh/security"
	"snail_tool/internal/system"
)

var (
	managedSSHDIncludeBegin, managedSSHDIncludeEnd = security.ManagedSSHDIncludeMarkers()
	customSSHDConfigPath                           = security.CustomSSHDConfigPath()
	sshdConfigPath                                 = security.SSHDConfigPath()
)

func Run(_ *system.Account) error {
	sshServiceChanged, err := cleanupSSHDConfig()
	if err != nil {
		return err
	}
	if sshServiceChanged {
		if err := security.ReloadService(); err != nil {
			return fmt.Errorf("重新加载 SSH 服务失败: %w", err)
		}
	}
	return nil
}

func cleanupSSHDConfig() (bool, error) {
	serviceChanged := false
	data, exists, err := shared.ReadRegularFile(customSSHDConfigPath)
	if err != nil {
		return false, err
	}
	begin, end := security.ManagedSSHDConfigMarkers()
	cleaned, err := shared.RemoveManagedBlockChecked(string(data), begin, end)
	if err != nil {
		return false, err
	}

	includeChanged, err := cleanupSSHDInclude()
	if err != nil {
		return serviceChanged, err
	}
	serviceChanged = serviceChanged || includeChanged

	if !exists {
		log.Info("未发现 SSH 自定义配置，跳过")
		return serviceChanged, nil
	}

	if !security.IsManagedSSHDConfigContent(string(data)) {
		log.Warn("SSH 自定义配置不是本工具生成，已跳过：", customSSHDConfigPath)
		return serviceChanged, nil
	}

	if err := shared.AtomicWriteFile(customSSHDConfigPath, []byte(shared.NormalizeCleanedContent(cleaned)), shared.AtomicWriteOptions{Mode: 0644, ForceMode: true}); err != nil {
		return serviceChanged, err
	}
	log.Info("已移除 SSH 自定义配置中的托管区块：", customSSHDConfigPath)
	return true, nil
}

func cleanupSSHDInclude() (bool, error) {
	data, exists, err := shared.ReadRegularFile(sshdConfigPath)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}

	content := string(data)
	cleaned, err := shared.RemoveManagedBlockChecked(content, managedSSHDIncludeBegin, managedSSHDIncludeEnd)
	if err != nil {
		return false, err
	}
	if cleaned == content {
		return false, nil
	}
	cleaned = shared.NormalizeCleanedContent(cleaned)

	if err := shared.AtomicWriteFile(sshdConfigPath, []byte(cleaned), shared.AtomicWriteOptions{Mode: 0644}); err != nil {
		return false, err
	}
	log.Info("已清理本工具写入的 SSH Include 配置")
	return true, nil
}
