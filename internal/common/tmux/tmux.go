package tmux

import (
	"os"
	"path/filepath"
	"strings"

	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

const (
	tmuxBegin   = "# ===== BEGIN SNAIL TMUX CONFIG ====="
	tmuxEnd     = "# ===== END SNAIL TMUX CONFIG ====="
	tmuxContent = `# 状态栏位置
set -g status on
set -g status-position bottom

# 整体颜色
set -g status-style "bg=#1e1e2e,fg=#cdd6f4"

# 左侧：会话名称
set -g status-left-length 30
set -g status-left "#[fg=#89b4fa,bold] 󰆍 #S "

# 当前窗口：蓝色背景
set -g window-status-current-style "bg=#89b4fa,fg=#11111b,bold"
set -g window-status-current-format " #I:#W#F "

# 其他窗口
set -g window-status-style "bg=#313244,fg=#cdd6f4"
set -g window-status-format " #I:#W "

# 窗口之间增加空格
set -g window-status-separator "  "

# 右侧：主机名 + 时间
set -g status-right-length 60
set -g status-right "#[fg=#a6e3a1]#H  #[fg=#f9e2af]%H:%M  #[fg=#cdd6f4]%Y-%m-%d "
`
)

func IsTmuxConfigured(account *system.Account) bool {
	content := shared.ReadFileString(filepath.Join(account.Home, ".tmux.conf"))
	managed, ok := shared.ManagedBlockContent(content, tmuxBegin, tmuxEnd)
	return ok && strings.TrimSpace(managed) == strings.TrimSpace(tmuxContent)
}

func Run() error {
	account, err := system.CurrentTargetUser()
	if err != nil {
		return err
	}
	if err := configureTmux(account); err != nil {
		return err
	}
	ui.PrintSuccessCard("tmux 配置完成",
		ui.CardField{Label: "配置文件", Value: filepath.Join(account.Home, ".tmux.conf")},
		ui.CardField{Label: "生效方式", Value: "新建 tmux 服务时自动加载；已有会话执行 tmux source-file ~/.tmux.conf"},
	)
	return nil
}

func configureTmux(account *system.Account) error {
	path := filepath.Join(account.Home, ".tmux.conf")
	options := shared.AtomicWriteOptions{
		Mode: 0644, Owner: &shared.FileOwner{UID: account.UID, GID: account.GID},
	}
	if err := shared.EnsureFileWithOptions(path, options); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	content, err := shared.RemoveManagedBlockChecked(string(data), tmuxBegin, tmuxEnd)
	if err != nil {
		return err
	}
	block := shared.FormatManagedBlock(tmuxBegin, tmuxContent, tmuxEnd)
	return shared.AtomicWriteFile(path, []byte(shared.AppendBlock(content, block)), options)
}

func Cleanup(account *system.Account) error {
	path := filepath.Join(account.Home, ".tmux.conf")
	if _, exists, err := shared.ReadRegularFile(path); err != nil || !exists {
		return err
	}
	_, err := shared.CleanupManagedBlocks(path, shared.BlockMarker{Begin: tmuxBegin, End: tmuxEnd})
	return err
}
