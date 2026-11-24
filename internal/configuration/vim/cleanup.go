package vim

import (
	"path/filepath"

	"snail_tool/internal/log"
	"snail_tool/internal/storage"
	"snail_tool/internal/system"
)

func Cleanup(account *system.Account) error {
	vimrc := filepath.Join(account.Home, ".vimrc")
	data, exists, err := storage.ReadRegularFile(vimrc)
	if err != nil {
		return err
	}
	if !exists {
		log.Info("未发现 Vim 配置，跳过")
		return nil
	}

	content := string(data)
	begin, end := VimMarkers()
	if _, err := storage.RemoveManagedBlockChecked(content, begin, end); err != nil {
		return err
	}
	if _, hasManagedBlock := storage.ManagedBlockContent(content, begin, end); !hasManagedBlock {
		if !IsManagedVimConfigContent(content) {
			log.Info("未发现 Vim 托管配置，跳过")
			return nil
		}
		// 兼容清理旧版本整份写入的模板。
		if err := storage.AtomicWriteFile(vimrc, nil, storage.AtomicWriteOptions{Mode: 0644}); err != nil {
			return err
		}
		log.Info("已清理旧版 Vim 配置：", vimrc)
		return nil
	}

	changed, err := storage.CleanupManagedBlocks(vimrc, storage.BlockMarker{Begin: begin, End: end})
	if err != nil {
		return err
	}
	if changed {
		log.Info("已清理 Vim 托管配置：", vimrc)
	}
	return nil
}
