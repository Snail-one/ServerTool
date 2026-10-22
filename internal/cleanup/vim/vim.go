package vim

import (
	"path/filepath"

	commonvim "snail_tool/internal/common/vim"
	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
)

func Run(account *system.Account) error {
	vimrc := filepath.Join(account.Home, ".vimrc")
	data, exists, err := shared.ReadRegularFile(vimrc)
	if err != nil {
		return err
	}
	if !exists {
		log.Info("未发现 Vim 配置，跳过")
		return nil
	}

	content := string(data)
	begin, end := commonvim.VimMarkers()
	if _, err := shared.RemoveManagedBlockChecked(content, begin, end); err != nil {
		return err
	}
	if _, hasManagedBlock := shared.ManagedBlockContent(content, begin, end); !hasManagedBlock {
		if !commonvim.IsManagedVimConfigContent(content) {
			log.Info("未发现 Vim 托管配置，跳过")
			return nil
		}
		// 兼容清理旧版本整份写入的模板。
		if err := shared.AtomicWriteFile(vimrc, nil, shared.AtomicWriteOptions{Mode: 0644}); err != nil {
			return err
		}
		log.Info("已清理旧版 Vim 配置：", vimrc)
		return nil
	}

	changed, err := shared.CleanupManagedBlocks(vimrc, shared.BlockMarker{Begin: begin, End: end})
	if err != nil {
		return err
	}
	if changed {
		log.Info("已清理 Vim 托管配置：", vimrc)
	}
	return nil
}
