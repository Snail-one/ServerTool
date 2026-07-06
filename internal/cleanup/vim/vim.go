package vim

import (
	"os"
	"path/filepath"

	commonvim "snail_tool/internal/common/vim"
	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
)

var vimrcContent = commonvim.ManagedVimConfigContent()

func Run(account *system.Account) error {
	vimrc := filepath.Join(account.Home, ".vimrc")
	if !system.FileExists(vimrc) {
		log.Info("未发现 Vim 配置，跳过")
		return nil
	}

	content := shared.ReadFileString(vimrc)
	if !commonvim.IsManagedVimConfigContent(content) {
		log.Warn("当前 Vim 配置与本工具模板不完全一致，已跳过：", vimrc)
		return nil
	}

	if err := os.WriteFile(vimrc, nil, 0644); err != nil {
		return err
	}
	log.Info("已清空 Vim 配置：", vimrc)
	return nil
}
