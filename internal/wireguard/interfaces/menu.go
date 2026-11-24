package interfaces

import (
	"fmt"
	"strings"

	"snail_tool/internal/ui"
)

type Snapshot struct {
	Fields                                                []ui.CardField
	AutostartStatus                                       string
	Saved, Running, AutostartAvailable, AutostartEnabled  bool
	CanChangeRuntime, CanRotate, CanDelete                bool
	Peers, Show, Edit, Autostart, Monitor, Rotate, Delete func() error
	Runtime                                               func(string) error
}

func Run(view *ui.UI, name string, load func() (Snapshot, error)) error {
	for {
		snapshot, err := load()
		if err != nil {
			return err
		}
		ui.ClearScreen()
		ui.MenuTitle("WireGuard", name)
		ui.PrintInfoCard("WireGuard 接口", snapshot.Fields...)

		actions := make(map[string]func() error)
		add := func(key, label string, action func() error) {
			ui.MenuOption(key, label)
			actions[key] = action
		}
		if snapshot.Saved {
			add("1", "管理对端", snapshot.Peers)
			add("2", "查看配置", snapshot.Show)
			add("3", "编辑配置", snapshot.Edit)
		}

		if snapshot.CanChangeRuntime {
			fmt.Println()
			label, action := "启动接口", "start"
			state := "已停止"
			if snapshot.Running {
				label, action = "停止接口", "stop"
				state = "运行中"
			}
			ui.MenuShortcutStatusHint("r", label, ui.SoftwareBadge(state, snapshot.Running), "")
			actions["r"] = func() error { return snapshot.Runtime(action) }
			if snapshot.Running {
				ui.MenuShortcutHint("d", "重启接口", "")
				actions["d"] = func() error { return snapshot.Runtime("restart") }
			}
		}

		if !snapshot.Saved && !snapshot.Running {
			return ui.ErrReturnToMenu
		}
		if snapshot.Running || snapshot.Saved && (snapshot.AutostartAvailable || snapshot.CanRotate || snapshot.CanDelete) {
			fmt.Println()
		}
		if snapshot.Saved && snapshot.AutostartAvailable {
			label := "开启开机自启"
			if snapshot.AutostartEnabled {
				label = "关闭开机自启"
			}
			ui.MenuShortcutStatusHint("a", label, snapshot.AutostartStatus, "")
			actions["a"] = snapshot.Autostart
		}
		if snapshot.Running {
			ui.MenuShortcutHint("c", "查看运行状态", "每秒刷新")
			actions["c"] = snapshot.Monitor
		}
		if snapshot.Saved {
			if snapshot.CanRotate {
				ui.MenuShortcutHint("k", "更换密钥", "更换接口私钥，可选择同时更换预共享密钥")
				actions["k"] = snapshot.Rotate
			}
			if snapshot.CanDelete {
				ui.MenuShortcutHint("x", "删除配置", "")
				actions["x"] = snapshot.Delete
			}
		}
		ui.MenuExit("0/q", "返回 WireGuard")
		choice, err := view.Ask("请选择：")
		if err != nil {
			return err
		}
		if ui.IsReturnChoice(choice) {
			return ui.ErrReturnToMenu
		}
		if choice == "" {
			continue
		}
		if action := actions[strings.ToLower(choice)]; action != nil {
			ui.RunAction(view, "WireGuard 操作失败，已返回接口菜单", action)
		} else {
			ui.InvalidChoice()
			view.Pause()
		}
	}
}
