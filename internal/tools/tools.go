package tools

import (
	"fmt"
	"os"
	"strings"

	"snail_tool/internal/app/startup"
	"snail_tool/internal/system"
	"snail_tool/internal/tools/commands"
	"snail_tool/internal/tools/golang"
	"snail_tool/internal/tools/startupinfo"
	"snail_tool/internal/tools/unattended"
	"snail_tool/internal/tools/ups"
	"snail_tool/internal/ui"
)

// Run displays system tools and Go environment management.
func Run(view *ui.UI, report *startup.Report) error {
	inventory := commands.NewInventory()
	distribution := distributionName()
	for {
		ui.ClearScreen()
		ui.MenuTitle("系统工具")
		packageManager := inventory.PackageManagerName()
		if packageManager == "" {
			packageManager = "未识别"
		}
		installed, total, detectionErr := inventory.InstalledCount()
		toolStatus := ui.SoftwareBadge(fmt.Sprintf("已安装 %d/%d", installed, total), installed > 0)
		if detectionErr != nil {
			toolStatus = ui.SoftwareBadge("检测失败", false)
		}
		goVersion := golang.CurrentVersion()
		goStatus := "未安装"
		if goVersion != "" {
			goStatus = "已安装 " + goVersion
		}
		goBadge := ui.SoftwareBadge(goStatus, goVersion != "")
		updates, updatesActive := unattended.StatusField()
		upsBadge := ui.ConfiguredBadge(ups.IsUPSConfigured())
		ui.PrintInfoCard("当前系统",
			ui.CardField{Label: "发行版", Value: distribution},
			ui.CardField{Label: "包管理器", Value: packageManager},
			ui.CardField{Label: "常用命令行工具", Value: toolStatus},
			ui.CardField{Label: "Go 语言环境", Value: goBadge},
			updates,
			ui.CardField{Label: "UPS（NUT）", Value: upsBadge},
		)
		fmt.Println()
		ui.MenuOptionStatus("1", "常用命令行工具", toolStatus)
		ui.MenuOptionStatus("2", "Go 语言环境", goBadge)
		ui.MenuOptionStatus("3", unattended.Title, ui.SoftwareBadge(updates.Value, updatesActive))
		fmt.Println()
		ui.MenuOptionStatus("4", "UPS（NUT）", upsBadge)
		fmt.Println()
		ui.MenuShortcutHint("c", "查看本次启动信息", "")
		ui.MenuExit("0/q", "返回")

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
			ui.RunAction(view, "常用命令行工具管理失败，已返回系统工具菜单", func() error {
				return inventory.Run(view)
			})
		case "2":
			ui.RunAction(view, "Go 语言环境管理失败，已返回系统工具菜单", func() error {
				return golang.Run(view)
			})
		case "3":
			ui.RunAction(view, "自动更新管理失败，已返回系统工具菜单", func() error {
				return unattended.Run(view)
			})
		case "4":
			ui.RunAction(view, "UPS 配置失败，已返回系统工具菜单", func() error {
				return ups.Run(view)
			})
		case "c":
			ui.RunAction(view, "查看启动信息失败，已返回系统工具菜单", func() error {
				return startupinfo.Show(report)
			})
		default:
			ui.InvalidChoice()
			view.Pause()
		}
	}
}

func distributionName() string {
	data, err := os.ReadFile("/etc/os-release")
	if os.IsNotExist(err) {
		data, err = os.ReadFile("/usr/lib/os-release")
	}
	if err != nil {
		return "未知"
	}
	fields := system.ParseOSRelease(string(data))
	for _, key := range []string{"PRETTY_NAME", "NAME", "ID"} {
		if fields[key] != "" {
			return fields[key]
		}
	}
	return "未知"
}
