package firewall

import (
	"fmt"

	"snail_tool/internal/log"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

var (
	commandExists = system.CommandExists
	commandRun    = system.Run
	commandOutput = system.Output
	isRoot        = system.IsRoot
)

func ensureNftables(view *ui.UI) (bool, error) {
	if !commandExists("nft") {
		confirmed, err := view.Confirm("未检测到 nft，是否安装 nftables？(y/N，0/q 返回)：")
		if err != nil {
			return false, err
		}
		if !confirmed {
			log.Info("已取消防火墙配置")
			return false, nil
		}
		if err := installNftables(); err != nil {
			return false, err
		}
		if !commandExists("nft") {
			return false, fmt.Errorf("安装后仍未检测到 nft，请检查 nftables 安装情况")
		}
	}
	return true, nil
}

func installNftables() error {
	for _, manager := range []struct {
		name string
		args []string
	}{
		{"apt-get", []string{"install", "-y", "nftables"}},
		{"apt", []string{"install", "-y", "nftables"}},
		{"dnf", []string{"install", "-y", "nftables"}},
		{"yum", []string{"install", "-y", "nftables"}},
		{"pacman", []string{"-S", "--noconfirm", "--needed", "nftables"}},
		{"zypper", []string{"--non-interactive", "install", "nftables"}},
		{"apk", []string{"add", "nftables"}},
	} {
		if !commandExists(manager.name) {
			continue
		}
		if manager.name == "apt" || manager.name == "apt-get" {
			if err := commandRun(manager.name, "update"); err != nil {
				return fmt.Errorf("更新软件包索引失败: %w", err)
			}
		}
		return commandRun(manager.name, manager.args...)
	}
	return fmt.Errorf("未识别支持的包管理器，请手动安装 nftables")
}
