package rootlogin

import (
	"snail_tool/internal/log"
	"snail_tool/internal/ui"
)

func Run(view *ui.UI, current func() (string, error), update func(string) error) error {
	value, err := current()
	if err != nil {
		return err
	}
	permitRootLogin, err := promptRootLoginPolicy(view, value)
	if err != nil {
		return err
	}
	if err := update(permitRootLogin); err != nil {
		return err
	}
	log.Info("Root SSH 登录策略已更新；请保留当前会话，新开终端测试登录。")
	return nil
}

func promptRootLoginPolicy(view *ui.UI, current string) (string, error) {
	permitRootLogin := "prohibit-password"
	err := ui.RunPromptSteps(
		ui.PromptStep{Run: func() error {
			ui.PrintField("当前 Root SSH 登录", current)
			ui.MenuOption("1", "仅允许 root 公钥登录")
			ui.MenuOption("2", "禁用 root SSH 登录（包括公钥）")
			ui.MenuExit("0/q", "返回 SSH 管理")
			for {
				choice, err := view.Ask("请选择：")
				if err != nil {
					return err
				}
				if ui.IsReturnChoice(choice) {
					return ui.ErrReturnToMenu
				}
				switch choice {
				case "1":
					permitRootLogin = "prohibit-password"
					return nil
				case "2":
					permitRootLogin = "no"
					return nil
				default:
					ui.InvalidChoice()
				}
			}
		}},
		ui.PromptStep{Run: func() error {
			var confirmed bool
			var err error
			if permitRootLogin == "no" {
				confirmed, err = confirmDisableRootLogin(view)
			} else {
				log.Warn("root 需有可用公钥才能登录；此操作不会添加公钥，也不会开放 root 密码登录。")
				confirmed, err = view.Confirm("确认仅允许 root 公钥登录？(y/N，0/q 返回上一步)：")
			}
			if err != nil {
				return err
			}
			if !confirmed {
				return ui.ErrCancelAction
			}
			return nil
		}},
	)
	return permitRootLogin, err
}

func confirmDisableRootLogin(view *ui.UI) (bool, error) {
	log.Warn("禁用后 root 将无法通过 SSH 新建连接，即使已配置公钥。工具无法验证普通用户是否已实际登录成功。")
	return view.Confirm("是否已在另一个终端使用普通用户公钥登录成功、确认 sudo 可用，并确认禁用 root SSH 登录？(y/N)：")
}
