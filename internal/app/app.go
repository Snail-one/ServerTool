package app

import (
	"fmt"
	"strings"

	"snail_tool/internal/config"
	"snail_tool/internal/log"
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
		showMenu()
		fmt.Println()

		choice, err := a.ui.Ask("输入选项: ")
		if err != nil {
			return err
		}
		fmt.Println()

		switch strings.ToLower(choice) {
		case "1":
			a.runAction("SSH 配置失败，已返回菜单", func() error {
				return config.ConfigureSSH(a.ui)
			})
		case "2":
			a.runAction("Vim 配置失败，已返回菜单", func() error {
				return config.ConfigureVim(a.ui)
			})
		case "3":
			a.runAction("Bash 配置失败，已返回菜单", config.ConfigureBash)
		case "4":
			a.runAction("代理配置失败，已返回菜单", func() error {
				return config.ConfigureProxy(a.ui)
			})
		case "q", "exit":
			fmt.Println("已退出")
			return nil
		default:
			fmt.Println("无效选项，请重新输入")
			a.ui.Pause()
		}
	}
}

func (a *App) runAction(failureMessage string, action func() error) {
	if err := action(); err != nil {
		log.Error(err)
		log.Error(failureMessage)
	}
	a.ui.Pause()
}

func showMenu() {
	fmt.Println("请选择操作：")
	fmt.Println("1) 配置当前用户 SSH 公钥登录 + 禁用密码登录 + 随机 SSH 端口")
	fmt.Println("2) 配置当前用户 Vim ~/.vimrc")
	fmt.Println("3) 配置当前用户 Bash 环境")
	fmt.Println("4) 配置当前用户 HTTP/HTTPS 代理环境变量")
	fmt.Println("q) 退出")
}
