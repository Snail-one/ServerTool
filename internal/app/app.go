package app

import (
	"fmt"
	"strings"

	"snail_tool/internal/cleanup"
	"snail_tool/internal/common"
	"snail_tool/internal/container"
	"snail_tool/internal/environment"
	"snail_tool/internal/quicksetup"
	"snail_tool/internal/shared"
	"snail_tool/internal/ssh"
	"snail_tool/internal/status"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
	"snail_tool/internal/version"
)

type App struct {
	ui *ui.UI
}

func New() *App {
	return &App{ui: ui.New()}
}

func (a *App) Run() error {
	for {
		ui.ClearScreen()
		showMenu(currentStatus())
		fmt.Println()

		choice, err := a.ui.Ask("输入选项: ")
		if err != nil {
			return err
		}
		fmt.Println()

		if shared.IsReturnChoice(choice) {
			fmt.Println("已退出")
			return nil
		}

		switch strings.ToLower(choice) {
		case "1":
			shared.RunAction(a.ui, "容器管理失败，已返回菜单", func() error {
				return container.Run(a.ui)
			})
		case "2":
			shared.RunAction(a.ui, "一键配置失败，已返回菜单", func() error {
				return quicksetup.Run(a.ui)
			})
		case "3":
			shared.RunAction(a.ui, "SSH 管理失败，已返回菜单", func() error {
				return ssh.Run(a.ui)
			})
		case "4":
			shared.RunAction(a.ui, "系统与用户配置失败，已返回菜单", func() error {
				return common.Run(a.ui)
			})
		case "5":
			shared.RunAction(a.ui, "开发环境管理失败，已返回菜单", func() error {
				return environment.Run(a.ui)
			})
		case "6":
			shared.RunAction(a.ui, "清理本工具配置失败，已返回菜单", func() error {
				return cleanup.Run(a.ui)
			})
		default:
			fmt.Println("无效选项，请重新输入")
			a.ui.Pause()
		}
	}
}

func currentStatus() status.Status {
	account, err := system.CurrentTargetUser()
	if err != nil {
		return status.DetectStatus(nil)
	}
	return status.DetectStatus(account)
}

func showMenu(status status.Status) {
	quickConfigured := quickSetupConfigured(status)
	runtimeConfigured := strings.TrimSpace(status.Runtime) != "" && status.Runtime != "未安装"
	ui.HomeTitle(version.Version)
	ui.MenuOption("1", "容器管理 "+ui.Badge(defaultStatus(status.Runtime, "未安装"), runtimeConfigured))
	ui.MenuOption("2", fmt.Sprintf("一键配置 %s", ui.Badge(fmt.Sprintf("已配置 %d/4", quickConfigured), quickConfigured == 4)))
	ui.MenuOption("3", "SSH 管理 "+ui.ConfiguredBadge(status.SSH))
	ui.MenuOption("4", fmt.Sprintf("系统与用户配置 %s", ui.Badge(fmt.Sprintf("已配置 %d/%d", status.Configured, status.ConfigTotal), status.ConfigTotal > 0 && status.Configured == status.ConfigTotal)))
	ui.MenuOption("5", "开发环境管理 "+ui.Badge("Go "+defaultStatus(status.GoVersion, "未配置"), status.GoVersion != ""))
	ui.MenuOption("6", "清理本工具配置")
	ui.MenuExit("0/q", "退出")
}

func quickSetupConfigured(status status.Status) int {
	configured := 0
	for _, item := range []bool{status.SSHKeys, status.SSHSecurity, status.Vim, status.Bash} {
		if item {
			configured++
		}
	}
	return configured
}

func defaultStatus(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
