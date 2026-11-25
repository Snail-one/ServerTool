package podman

import (
	"fmt"

	"snail_tool/internal/log"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

func Install(view *ui.UI, ensureAbsent func() error) error {
	if !system.IsRoot() {
		return fmt.Errorf("安装 Podman 需要 root 权限，请使用 sudo 运行本工具")
	}

	var manager string
	switch {
	case system.CommandExists("apt"):
		manager = "apt"
	case system.CommandExists("apt-get"):
		manager = "apt-get"
	default:
		return fmt.Errorf("未找到 apt 或 apt-get，无法自动安装 Podman")
	}
	confirmed, err := view.Confirm(fmt.Sprintf("使用 %s 更新软件包索引并安装 Podman？(y/N，0/q 返回)：", manager))
	if err != nil {
		return err
	}
	if !confirmed {
		return ui.ErrReturnToMenu
	}
	if err := ensureAbsent(); err != nil {
		return err
	}

	log.Info("安装 Podman...")
	if err := system.Run(manager, "update"); err != nil {
		return fmt.Errorf("%s update 失败: %w", manager, err)
	}
	return system.Run(manager, "install", "-y", "podman")
}
