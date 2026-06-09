package config

import (
	"fmt"
	"os"
	"path/filepath"

	"snail_tool/internal/log"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

const vimrcContent = `" --- 第一步：加载官方默认设置 ---
if !exists('g:skip_defaults_vim')
  source $VIMRUNTIME/defaults.vim
endif

" --- 第二步：写你自己的『覆盖』命令 ---
set mouse=
set pastetoggle=<F2>
nnoremap <F3> :set number!<CR>
`

func ConfigureVim(view *ui.UI) error {
	account, err := system.CurrentTargetUser()
	if err != nil {
		return err
	}

	fmt.Println("检查 vim 是否安装...")
	if !system.CommandExists("vim") {
		log.Info("vim 未安装，正在安装...")
		if err := installVim(); err != nil {
			return err
		}
	} else {
		log.Info("vim 已安装")
	}

	vimrc := filepath.Join(account.Home, ".vimrc")
	fmt.Println()

	if system.FileNonEmpty(vimrc) {
		fmt.Printf("检测到已有 Vim 配置：%s\n", vimrc)
		confirmed, err := view.Confirm("是否覆盖现有配置？(y/N): ")
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Println("已取消覆盖")
			return nil
		}

		backup, err := system.Backup(vimrc)
		if err != nil {
			return err
		}
		fmt.Printf("已备份原配置：%s\n\n", backup)
	}

	fmt.Println("写入 ~/.vimrc ...")
	if err := os.WriteFile(vimrc, []byte(vimrcContent), 0644); err != nil {
		return err
	}
	if err := system.ChownPath(vimrc, account, false); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("Vim 配置完成")
	fmt.Printf("配置文件：%s\n", vimrc)
	return nil
}

func installVim() error {
	switch {
	case system.CommandExists("apt"):
		if err := system.Run("apt", "update"); err != nil {
			return err
		}
		return system.Run("apt", "install", "-y", "vim")
	case system.CommandExists("dnf"):
		return system.Run("dnf", "install", "-y", "vim")
	case system.CommandExists("yum"):
		return system.Run("yum", "install", "-y", "vim")
	case system.CommandExists("pacman"):
		return system.Run("pacman", "-Sy", "--noconfirm", "vim")
	default:
		return fmt.Errorf("无法识别包管理器，请手动安装 vim")
	}
}
