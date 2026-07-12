package sshkeys

import (
	"os"
	"path/filepath"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/ssh/keys"
	"snail_tool/internal/system"
)

var sshAuthorizedKeysBegin, sshAuthorizedKeysEnd = keys.Markers()

func Run(account *system.Account) error {
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
	cleaned := shared.RemoveManagedBlock(content, sshAuthorizedKeysBegin, sshAuthorizedKeysEnd)
	if cleaned == content {
		log.Warn("未发现由本工具标记的 SSH 公钥，已保留 authorized_keys")
		return nil
	}

	cleaned = shared.NormalizeCleanedContent(cleaned)
	if err := shared.AtomicWriteFile(authKeys, []byte(cleaned), shared.AtomicWriteOptions{Mode: 0600, ForceMode: true}); err != nil {
		return err
	}

	log.Info("已清理本工具写入的 SSH 公钥块")
	return nil
}
