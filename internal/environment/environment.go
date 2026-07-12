package environment

import (
	"fmt"
	"strings"

	"snail_tool/internal/environment/golang"
	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

func Run(view *ui.UI) error {
	for {
		ui.ClearScreen()
		fmt.Println("请选择环境配置：")
		fmt.Println("1) Go 语言")
		fmt.Println("0/q) 返回")
		fmt.Println()

		choice, err := view.Ask("输入选项: ")
		if err != nil {
			return err
		}
		fmt.Println()

		switch strings.ToLower(strings.TrimSpace(choice)) {
		case "1":
			shared.RunAction(view, "Go 语言环境管理失败，已返回环境配置", func() error {
				return golang.Run(view)
			})
		case "0", "q", "exit":
			return shared.ErrReturnToMenu
		default:
			fmt.Println("无效选项，请重新输入")
			view.Pause()
		}
	}
}
