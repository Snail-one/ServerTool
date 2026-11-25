package rootlogin

import (
	"fmt"

	"snail_tool/internal/log"
	"snail_tool/internal/ui"
)

func Run(view *ui.UI, current func() (string, error), update func(string) error) error {
	for {
		ui.ClearScreen()
		value, readErr := current()
		choices := map[string]string{"r": "refresh"}
		if readErr == nil {
			choices["1"] = "prohibit-password"
			choices["2"] = "no"
		}
		draw := func() {
			ui.MenuTitle("SSH 管理", "Root SSH 登录策略")
			if readErr != nil {
				ui.PrintWarningCard("暂时无法调整 Root SSH 登录策略", ui.CardField{Label: "原因", Value: readErr.Error()})
			} else {
				ui.PrintInfoCard("当前 Root SSH 登录", ui.CardField{Label: "登录策略", Value: value})
				fmt.Println()
				ui.MenuOptionStatus("1", "仅允许 root 公钥登录", ui.CurrentBadge(value == "仅允许密钥登录", true))
				ui.MenuOptionStatus("2", "禁用 root SSH 登录（包括公钥）", ui.CurrentBadge(value == "已禁止", false))
			}
			fmt.Println()
			ui.MenuShortcutHint("r", "刷新状态", "")
			ui.MenuExit("0/q", "返回 SSH 管理")
		}
		choice, err := ui.AskMenuChoice(view, "请选择", "", choices, draw)
		if err != nil {
			return err
		}
		if choice == "refresh" {
			continue
		}
		ui.RunAction(view, "修改失败，已返回 Root SSH 登录策略", func() error {
			if err := applyPolicy(view, choice, update); err != nil {
				return err
			}
			log.Info("Root SSH 登录策略已更新。请保留当前会话，新开终端测试登录。")
			return nil
		})
	}
}

func applyPolicy(view *ui.UI, policy string, update func(string) error) error {
	var confirmed bool
	var err error
	if policy == "no" {
		confirmed, err = confirmDisableRootLogin(view)
	} else {
		log.Warn("root 需有可用公钥才能登录。系统将使用已有的 root 公钥。")
		confirmed, err = view.Confirm("确认仅允许 root 公钥登录？(y/N，0/q 返回)：")
	}
	if err != nil {
		return err
	}
	if !confirmed {
		return ui.ErrReturnToMenu
	}
	return update(policy)
}

func confirmDisableRootLogin(view *ui.UI) (bool, error) {
	log.Warn("禁用后 root 将无法通过 SSH 新建连接，即使已配置公钥。工具无法验证普通用户是否已实际登录成功。")
	return view.Confirm("是否已在另一个终端使用普通用户公钥登录成功、确认 sudo 可用，并确认禁用 root SSH 登录？(y/N，0/q 返回)：")
}
