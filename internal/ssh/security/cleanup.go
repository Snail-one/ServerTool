package security

import (
	"fmt"

	"snail_tool/internal/log"
	"snail_tool/internal/storage"
	"snail_tool/internal/system"
)

func Cleanup(_ *system.Account) error {
	sshServiceChanged, err := cleanupSSHDConfig(sshdConfigPath, customSSHDConfigPath)
	if err != nil {
		return err
	}
	if sshServiceChanged {
		if err := reloadSSHService(); err != nil {
			return fmt.Errorf("重新加载 SSH 服务失败: %w", err)
		}
	}
	return nil
}

func cleanupSSHDConfig(sshdPath, customPath string) (bool, error) {
	serviceChanged := false
	data, exists, err := storage.ReadRegularFile(customPath)
	if err != nil {
		return false, err
	}
	begin, end := managedSSHDConfigBegin, managedSSHDConfigEnd
	cleaned, err := storage.RemoveManagedBlockChecked(string(data), begin, end)
	if err != nil {
		return false, err
	}

	includeChanged, err := cleanupSSHDInclude(sshdPath)
	if err != nil {
		return serviceChanged, err
	}
	serviceChanged = serviceChanged || includeChanged

	if !exists {
		log.Info("未发现 SSH 自定义配置，跳过")
		return serviceChanged, nil
	}

	if !isManagedSSHDConfig(string(data)) {
		log.Warn("SSH 自定义配置不是本工具生成，已跳过：", customPath)
		return serviceChanged, nil
	}

	if err := storage.AtomicWriteFile(customPath, []byte(storage.NormalizeCleanedContent(cleaned)), storage.AtomicWriteOptions{Mode: 0644, ForceMode: true}); err != nil {
		return serviceChanged, err
	}
	log.Info("已移除 SSH 自定义配置中的托管区块：", customPath)
	return true, nil
}

func cleanupSSHDInclude(sshdPath string) (bool, error) {
	data, exists, err := storage.ReadRegularFile(sshdPath)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}

	content := string(data)
	cleaned, err := storage.RemoveManagedBlockChecked(content, managedSSHDIncludeBegin, managedSSHDIncludeEnd)
	if err != nil {
		return false, err
	}
	if cleaned == content {
		return false, nil
	}
	cleaned = storage.NormalizeCleanedContent(cleaned)

	if err := storage.AtomicWriteFile(sshdPath, []byte(cleaned), storage.AtomicWriteOptions{Mode: 0644}); err != nil {
		return false, err
	}
	log.Info("已清理本工具写入的 SSH Include 配置")
	return true, nil
}
