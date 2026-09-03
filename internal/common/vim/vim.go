package vim

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

const vimSettings = `syntax on
filetype plugin indent on
set mouse=
set pastetoggle=<F2>
nnoremap <F3> :set number!<CR>
`

const legacyVimrcContent = `" --- 第一步：加载官方默认设置 ---
if !exists('g:skip_defaults_vim')
  source $VIMRUNTIME/defaults.vim
endif

" --- 第二步：写你自己的『覆盖』命令 ---
` + vimSettings

const vimrcContent = `" SNAIL TOOL 默认配置；请在托管区块外添加自定义配置
if !exists('g:skip_defaults_vim')
  source $VIMRUNTIME/defaults.vim
endif

` + vimSettings

const (
	vimrcBegin = `" ===== BEGIN SNAIL VIM CONFIG =====`
	vimrcEnd   = `" ===== END SNAIL VIM CONFIG =====`
)

func VimMarkers() (string, string) {
	return vimrcBegin, vimrcEnd
}

func ManagedVimConfigContent() string {
	return vimrcContent
}

func IsVimConfigured(account *system.Account) bool {
	vimrc := filepath.Join(account.Home, ".vimrc")
	return IsManagedVimConfigContent(shared.ReadFileString(vimrc))
}

func IsManagedVimConfigContent(content string) bool {
	managed, ok := shared.ManagedBlockContent(content, vimrcBegin, vimrcEnd)
	if ok {
		return strings.TrimSpace(managed) == strings.TrimSpace(vimrcContent)
	}

	// 兼容旧版本整份写入的模板，下次配置时会自动迁移到托管区块。
	return strings.TrimSpace(content) == strings.TrimSpace(legacyVimrcContent)
}

func Run(view *ui.UI) error {
	return ConfigureVim(view)
}

func ConfigureVim(_ *ui.UI) error {
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

	if err := shared.EnsureFileWithOptions(vimrc, shared.AtomicWriteOptions{
		Mode: 0644, Owner: &shared.FileOwner{UID: account.UID, GID: account.GID},
	}); err != nil {
		return err
	}

	fmt.Println("更新 ~/.vimrc 中的 Vim 托管配置 ...")
	if err := replaceVimConfig(vimrc); err != nil {
		return err
	}
	if err := system.ChownPath(vimrc, account, false); err != nil {
		return err
	}

	fmt.Println()
	ui.PrintSuccessCard("Vim 配置完成", ui.CardField{Label: "配置文件", Value: vimrc})
	return nil
}

func replaceVimConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	content := string(data)
	if strings.TrimSpace(content) == strings.TrimSpace(legacyVimrcContent) {
		// 旧版本会将模板写成整个文件；迁移时避免保留一份重复配置。
		content = ""
	} else {
		content = shared.RemoveManagedBlock(content, vimrcBegin, vimrcEnd)
	}

	block := shared.FormatManagedBlock(vimrcBegin, vimrcContent, vimrcEnd)
	return shared.AtomicWriteFile(path, []byte(shared.AppendBlock(content, block)), shared.AtomicWriteOptions{Mode: 0644})
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
