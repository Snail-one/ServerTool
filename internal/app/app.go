package app

import (
	"errors"
	"fmt"
	"strings"

	"snail_tool/internal/config"
	"snail_tool/internal/log"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
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

		switch strings.ToLower(choice) {
		case "1":
			a.runAction("Docker Compose 应用更新失败，已返回菜单", func() error {
				return config.UpdateDockerComposeApps(a.ui)
			})
		case "2":
			a.runAction("SSH 公钥配置失败，已返回菜单", func() error {
				return config.ConfigureSSH(a.ui)
			})
		case "3":
			a.runAction("配置失败，已返回菜单", func() error {
				return a.configureFiles()
			})
		case "4":
			a.runAction("清理配置失败，已返回菜单", func() error {
				return config.CleanupConfig(a.ui)
			})
		case "0", "q", "exit":
			fmt.Println("已退出")
			return nil
		default:
			fmt.Println("无效选项，请重新输入")
			a.ui.Pause()
		}
	}
}

func (a *App) configureFiles() error {
	status := currentStatus()
	fmt.Println("请选择配置操作：")
	fmt.Println("1) SSH 常用安全配置" + statusText(status.SSHSecurity))
	fmt.Println("2) Vim ~/.vimrc" + statusText(status.Vim))
	fmt.Println("3) Bash 环境" + statusText(status.Bash))
	fmt.Println("4) HTTP/HTTPS 代理设置" + proxyStatusText(status.Proxy))
	fmt.Println("5) UPS 配置" + statusText(status.UPS))
	fmt.Println("6) 查看当前 SSH 安全配置")
	fmt.Println("0/q) 返回")
	fmt.Println()

	choice, err := a.ui.Ask("输入选项: ")
	if err != nil {
		return err
	}
	fmt.Println()

	switch strings.ToLower(choice) {
	case "1":
		return config.ConfigureSSHSecurity(a.ui)
	case "2":
		return config.ConfigureVim(a.ui)
	case "3":
		return config.ConfigureBash()
	case "4":
		return config.ConfigureProxy(a.ui)
	case "5":
		return config.ConfigureUPS(a.ui)
	case "6":
		return config.ShowSSHSecurityStatus()
	case "0", "q", "exit":
		return config.ErrReturnToMenu
	default:
		fmt.Println("无效选项，已返回菜单")
		return nil
	}
}

func (a *App) runAction(failureMessage string, action func() error) {
	if err := action(); err != nil {
		if errors.Is(err, config.ErrReturnToMenu) {
			return
		}
		log.Error(err)
		log.Error(failureMessage)
	}
	a.ui.Pause()
}

func currentStatus() config.Status {
	account, err := system.CurrentTargetUser()
	if err != nil {
		return config.Status{}
	}
	return config.DetectStatus(account)
}

func showMenu(status config.Status) {
	fmt.Println("请选择操作：")
	fmt.Println("1) 批量更新运行中的 Docker Compose 应用")
	fmt.Println("2) SSH 公钥管理" + statusText(status.SSHKeys))
	fmt.Println("3) 配置")
	fmt.Println("4) 清理配置")
	fmt.Println("0/q) 退出")
}

func statusText(configured bool) string {
	if configured {
		return " [已配置]"
	}
	return ""
}

func proxyStatusText(configured bool) string {
	if configured {
		return " [已配置代理]"
	}
	return ""
}
