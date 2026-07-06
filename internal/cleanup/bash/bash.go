package bash

import (
	"path/filepath"

	commonbash "snail_tool/internal/common/bash"
	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
)

const (
	legacyBashCommandBegin = "# ===== BEGIN SNAIL COMMAND ====="
	legacyBashCommandEnd   = "# ===== END SNAIL COMMAND ====="
)

var (
	bashAliasBegin, bashAliasEnd = commonbash.BashAliasMarkers()
	bashAliasBlock               = commonbash.BashAliasBlock()
)

func Run(account *system.Account) error {
	bashrc := filepath.Join(account.Home, ".bashrc")
	changed, err := shared.CleanupManagedBlocks(
		bashrc,
		shared.BlockMarker{Begin: bashAliasBegin, End: bashAliasEnd},
		shared.BlockMarker{Begin: legacyBashCommandBegin, End: legacyBashCommandEnd},
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
