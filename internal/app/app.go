package app

import (
	"fmt"
	"os"
	"strings"
	"time"

	"snail_tool/internal/app/startup"
	"snail_tool/internal/app/status"
	"snail_tool/internal/cleanup"
	"snail_tool/internal/configuration"
	"snail_tool/internal/container"
	"snail_tool/internal/firewall"
	"snail_tool/internal/ssh"
	"snail_tool/internal/system"
	"snail_tool/internal/tools"
	"snail_tool/internal/ui"
	"snail_tool/internal/version"
	"snail_tool/internal/wireguard"
)

type App struct {
	ui      *ui.UI
	startup *startup.Report
}

func NewWithStartupReport(report *startup.Report) *App {
	if report == nil {
		report = startup.New(version.Version)
	}
	return &App{ui: ui.New(), startup: report}
}

func (a *App) Run() error {
	ui.ClearScreen()
	ui.MenuTitle("状态检测")
	menuStatus := currentStatus(a.startup)
	invalidChoice := false
	for !a.ui.InputEnded() {
		ui.ClearScreen()
		showMenu(menuStatus)
		if invalidChoice {
			ui.InvalidChoice()
			invalidChoice = false
		}

		choice, err := a.ui.Ask("请选择：")
		if a.ui.InputEnded() {
			break
		}
		if err != nil {
			return err
		}
		fmt.Println()

		if ui.IsReturnChoice(choice) {
			break
		}

		switch strings.ToLower(choice) {
		case "1":
			ui.RunAction(a.ui, "防火墙配置失败，已返回菜单", func() error {
				return firewall.Run(a.ui)
			})
		case "2":
			ui.RunAction(a.ui, "SSH 管理失败，已返回菜单", func() error {
				return ssh.Run(a.ui)
			})
		case "3":
			ui.RunAction(a.ui, "配置管理失败，已返回菜单", func() error {
				return configuration.Run(a.ui)
			})
		case "4":
			ui.RunAction(a.ui, "系统工具菜单执行失败，已返回菜单", func() error {
				return tools.Run(a.ui, a.startup)
			})
		case "5":
			ui.RunAction(a.ui, "WireGuard 管理失败，已返回菜单", func() error {
				return wireguard.Run(a.ui)
			})
		case "6":
			ui.RunAction(a.ui, "容器管理失败，已返回菜单", func() error {
				return container.Manage(a.ui)
			})
		case "c":
			ui.RunAction(a.ui, "清理配置失败，已返回菜单", func() error {
				return cleanup.Run(a.ui)
			})
		default:
			invalidChoice = true
		}
	}
	if a.ui.InputEnded() {
		fmt.Println()
	}
	fmt.Println("已退出")
	return nil
}

func currentStatus(report *startup.Report) status.Status {
	started := time.Now()
	progress := ui.NewDetectionProgress(os.Stdout)
	step := func(label string, detect func()) {
		report.Record(label, func() { progress.Step(label, detect) })
	}
	var account *system.Account
	var userErr error
	step("用户信息（getent passwd）", func() {
		account, userErr = system.CurrentTargetUser()
		if userErr != nil {
			account = nil
		}
	})
	result := status.DetectStatusWithProgress(account, step)
	report.Complete(account, userErr, result)
	fmt.Printf("状态检测完成，总耗时 %.1f 秒\n\n", time.Since(started).Seconds())
	return result
}

func showMenu(status status.Status) {
	runtimeConfigured := strings.TrimSpace(status.Runtime) != "" && status.Runtime != "未安装"
	ui.HomeTitle(version.Version)
	ui.MenuOption("1", "防火墙管理")
	fmt.Println()
	ui.MenuOptionStatus("2", "SSH 管理", ui.ConfiguredBadge(status.SSH))
	ui.MenuOption("3", "配置管理")
	fmt.Println()
	ui.MenuOption("4", "系统工具")
	ui.MenuOptionStatus("5", "WireGuard", wireguard.Badge())
	ui.MenuOptionStatus("6", "容器管理", ui.SoftwareBadge(defaultStatus(status.Runtime, "未安装"), runtimeConfigured))
	fmt.Println()
	ui.MenuShortcutHint("c", "清理配置", "")
	ui.MenuExit("0/q", "退出")
}

func defaultStatus(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
