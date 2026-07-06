package common

import (
	"fmt"
	"strings"

	commonbash "snail_tool/internal/common/bash"
	commonproxy "snail_tool/internal/common/proxy"
	commonups "snail_tool/internal/common/ups"
	commonvim "snail_tool/internal/common/vim"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

func Run(view *ui.UI) error {
	ui.ClearScreen()
	status := currentStatus()
	fmt.Println("请选择配置操作：")
	fmt.Println("1) Vim ~/.vimrc" + statusText(status.vim))
	fmt.Println("2) Bash 环境" + statusText(status.bash))
	fmt.Println("3) HTTP/HTTPS 代理设置" + proxyStatusText(status.proxy))
	fmt.Println("4) UPS 配置" + statusText(status.ups))
	fmt.Println("0/q) 返回")
	fmt.Println()

	choice, err := view.Ask("输入选项: ")
	if err != nil {
		return err
	}
	fmt.Println()

	switch strings.ToLower(choice) {
	case "1":
		return commonvim.Run(view)
	case "2":
		return commonbash.Run()
	case "3":
		return commonproxy.Run(view)
	case "4":
		return commonups.Run(view)
	case "0", "q", "exit":
		return shared.ErrReturnToMenu
	default:
		fmt.Println("无效选项，已返回菜单")
		return nil
	}
}

type commonStatus struct {
	vim   bool
	bash  bool
	proxy bool
	ups   bool
}

func currentStatus() commonStatus {
	account, err := system.CurrentTargetUser()
	if err != nil {
		return commonStatus{}
	}
	return commonStatus{
		vim:   commonvim.IsVimConfigured(account),
		bash:  commonbash.IsBashConfigured(account),
		proxy: commonproxy.IsProxyConfigured(account),
		ups:   commonups.IsUPSConfigured(),
	}
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
