package docker

import (
	"fmt"
	"strings"

	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

func ChooseInstall(view *ui.UI, checkAvailable func() error) error {
	invalidChoice := false
	for {
		ui.ClearScreen()
		ui.MenuTitle("容器管理", "安装 Docker")
		fmt.Println("未检测到 Docker。")
		fmt.Println()
		ui.MenuSection("请选择安装方式")
		ui.MenuOptionHint("1", "安装 Docker", "Docker 官方签名 stable 仓库")
		ui.MenuOptionHint("2", "安装 Docker", "Docker 官方脚本 get.docker.com")
		ui.MenuExit("0/q", "返回")

		if invalidChoice {
			ui.InvalidChoice()
			invalidChoice = false
		}
		choice, err := view.Ask("请选择：")
		if err != nil {
			return err
		}
		fmt.Println()

		if ui.IsReturnChoice(choice) {
			return ui.ErrReturnToMenu
		}
		switch strings.ToLower(strings.TrimSpace(choice)) {
		case "1":
			return installFromRepository(view, checkAvailable)
		case "2":
			return installFromScript(view, checkAvailable)
		default:
			invalidChoice = true
		}
	}
}

func installFromRepository(view *ui.UI, checkAvailable func() error) error {
	if err := checkAvailable(); err != nil {
		return err
	}
	if !system.IsRoot() {
		return fmt.Errorf("安装 Docker 需要 root 权限，请使用 sudo 运行本工具")
	}
	return Install(view)
}

func installFromScript(view *ui.UI, checkAvailable func() error) error {
	if err := checkAvailable(); err != nil {
		return err
	}
	if !system.IsRoot() {
		return fmt.Errorf("使用官方脚本安装 Docker 需要 root 权限，请使用 sudo 运行本工具")
	}
	return InstallWithScript(view)
}
