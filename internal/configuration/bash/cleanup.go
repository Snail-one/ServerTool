package bash

import (
	"path/filepath"

	"snail_tool/internal/log"
	"snail_tool/internal/storage"
	"snail_tool/internal/system"
)

const (
	legacyBashCommandBegin = "# ===== BEGIN SNAIL COMMAND ====="
	legacyBashCommandEnd   = "# ===== END SNAIL COMMAND ====="
)

func Cleanup(account *system.Account) error {
	bashrc := filepath.Join(account.Home, ".bashrc")
	changed, err := storage.CleanupManagedBlocks(
		bashrc,
		storage.BlockMarker{Begin: bashAliasBegin, End: bashAliasEnd},
		storage.BlockMarker{Begin: legacyBashCommandBegin, End: legacyBashCommandEnd},
	)
	if err != nil {
		return err
	}
	if changed {
		log.Info("已清理 Bash 托管配置：", bashrc)
	} else {
		log.Info("未发现 Bash 托管配置，跳过")
	}
	return nil
}
