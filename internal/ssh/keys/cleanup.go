package keys

import (
	"path/filepath"

	"snail_tool/internal/log"
	"snail_tool/internal/storage"
	"snail_tool/internal/system"
)

func Cleanup(account *system.Account) error {
	authKeys := filepath.Join(account.Home, ".ssh", "authorized_keys")
	data, exists, err := storage.ReadRegularFile(authKeys)
	if err != nil {
		return err
	}
	if !exists {
		log.Info("未发现 SSH authorized_keys，跳过")
		return nil
	}

	content := string(data)
	cleaned, err := storage.RemoveManagedBlockChecked(content, sshAuthorizedKeysBegin, sshAuthorizedKeysEnd)
	if err != nil {
		return err
	}
	if cleaned == content {
		log.Warn("未发现由本工具标记的 SSH 公钥，已保留 authorized_keys")
		return nil
	}

	cleaned = storage.NormalizeCleanedContent(cleaned)
	if err := storage.AtomicWriteFile(authKeys, []byte(cleaned), storage.AtomicWriteOptions{Mode: 0600, ForceMode: true}); err != nil {
		return err
	}

	log.Info("已清理本工具写入的 SSH 公钥块")
	return nil
}
