package dockermark

import (
	"errors"

	"snail_tool/internal/log"
	"snail_tool/internal/ui"
)

type Actions struct {
	State         func() ui.CardField
	RequireDocker func() error
	Enable        func() (pending bool, err error)
	Disable       func() (bool, error)
	Unit, Mark    string
}

func run(view *ui.UI, actions Actions) error {
	for {
		ui.ClearScreen()
		ui.MenuTitle("防火墙管理", "转发规则管理", "Docker 放行标记")
		state := actions.State()
		ui.PrintInfoCard("Docker 放行标记",
			state,
			ui.CardField{Label: "作用", Value: "让 Docker 接受命中本工具转发端口或来源放行的连接"},
			ui.CardField{Label: "做法", Value: "带标记的连接先经过 DOCKER-USER 中已有的过滤规则"},
			ui.CardField{Label: "开机", Value: "Docker 每次启动后自动补上这条规则"},
			ui.CardField{Label: "不影响", Value: "没有标记的流量仍走 Docker 原来的规则"},
			ui.CardField{Label: "前提", Value: "需要 Docker 的 iptables 后端"},
		)
		ui.MenuOptionStatus("1", "开启", ui.CurrentBadge(state.Value == ui.PrimaryText("已生效")))
		ui.MenuOptionStatus("2", "关闭", ui.CurrentBadge(state.Value == "已关闭"))
		ui.MenuExit("0/q", "返回转发规则管理")
		choice, err := ui.AskChoice(view, "请选择", "", map[string]string{"1": "on", "2": "off"})
		if err != nil {
			return err
		}
		if choice == "on" {
			if err := actions.RequireDocker(); err != nil {
				return err
			}
			confirmed, err := ui.ConfirmMenuChange(view, "确认开启 Docker 放行标记？会更新防火墙转发规则并加载。", "转发规则管理")
			if errors.Is(err, ui.ErrReturnToMenu) {
				continue
			}
			if err != nil {
				return err
			}
			if !confirmed {
				return ui.ErrReturnToMenu
			}
			pending, err := actions.Enable()
			if pending {
				ui.PrintSuccessCard("Docker 放行标记已开启",
					ui.CardField{Label: "当前规则", Value: "等待 FORWARD 跳转到 DOCKER-USER，Docker 启动后会尝试同步"},
					ui.CardField{Label: "开机", Value: actions.Unit},
				)
				return nil
			}
			if err != nil {
				return err
			}
			log.Info("Docker 放行标记已开启")
			ui.PrintSuccessCard("Docker 放行标记已开启",
				ui.CardField{Label: "DOCKER-USER", Value: "已接受转发标记 " + actions.Mark},
				ui.CardField{Label: "开机", Value: actions.Unit},
			)
			return nil
		}
		confirmed, err := ui.ConfirmMenuChange(view, "确认关闭 Docker 放行标记？会移除打标规则和 Docker 中对应的接收规则。", "转发规则管理")
		if errors.Is(err, ui.ErrReturnToMenu) {
			continue
		}
		if err != nil {
			return err
		}
		if !confirmed {
			return ui.ErrReturnToMenu
		}
		removed, err := actions.Disable()
		if err != nil {
			return err
		}
		if !removed {
			ui.PrintInfoCard("无需修改", ui.CardField{Label: "开关", Value: "已关闭"})
			view.Pause()
			continue
		}
		ui.PrintSuccessCard("Docker 放行标记已关闭",
			ui.CardField{Label: "DOCKER-USER", Value: "已移除转发标记放行"},
			ui.CardField{Label: "防火墙标记", Value: "已从防火墙配置中去掉"},
		)
		return nil
	}
}
