package unattended

import (
	"fmt"
	"strings"

	"snail_tool/internal/ui"
)

func (service manager) menu(view *ui.UI) error {
	choices := map[string]string{
		"1": "enable", "2": "disable", "3": "options", "4": "schedule",
		"5": "edit", "6": "effective", "7": "simulate", "8": "run",
		"9": "logs", "r": "refresh", "x": "reset",
	}
	for {
		ui.ClearScreen()
		fields := service.fields()
		draw := func() {
			ui.MenuTitle("系统工具", "unattended-upgrades 管理")
			ui.PrintInfoCard("当前状态", fields...)
			fmt.Println()
			ui.MenuOptionHint("1", "开启自动更新", "如有需要，先安装 unattended-upgrades")
			ui.MenuOption("2", "关闭自动更新")
			ui.MenuOptionHint("3", "APT 策略与开关", "更新频率、重启、软件包清理、邮件报告")
			ui.MenuOptionHint("4", "设置定时器", "触发时刻、随机延迟、补跑")
			ui.MenuOptionHint("5", "编辑原生 APT 配置", "软件源、允许或阻止的软件包及其他原生参数")
			ui.MenuOption("6", "查看生效配置")
			fmt.Println()
			ui.MenuOptionHint("7", "模拟运行", "查看当前策略会选择哪些更新")
			ui.MenuOption("8", "立即执行一次更新")
			ui.MenuOption("9", "查看最近更新日志")
			ui.MenuOption("r", "刷新状态")
			ui.MenuShortcutHint("x", "恢复原有 APT 策略", "移除本工具写入的 APT 配置")
			ui.MenuExit("0/q", "返回系统工具")
		}
		choice, err := ui.AskMenuChoice(view, "请选择", "", choices, draw)
		if err != nil {
			return err
		}
		if choice == "refresh" {
			continue
		}
		ui.RunAction(view, "自动更新操作失败，已返回管理菜单", func() error {
			switch choice {
			case "options":
				return service.optionMenu(view)
			case "schedule":
				return service.scheduleMenu(view)
			case "edit":
				return service.editNative(view)
			case "effective":
				return service.showEffective()
			case "simulate", "run":
				return service.runUpdate(view, choice == "simulate")
			case "logs":
				return service.showLog()
			case "reset":
				return service.resetNative(view)
			}
			enable := choice == "enable"
			if err := service.change(view, enable); err != nil {
				return err
			}
			label := "已关闭自动更新"
			if enable {
				label = "已开启自动更新"
			}
			ui.PrintSuccessCard(label)
			return nil
		})
	}
}

func (service manager) showLog() error {
	output, err := service.query("apt-config", "shell", "LOG_DIR", "Unattended-Upgrade::LogDir/d")
	if err != nil {
		return err
	}
	dir := assignments(output)["LOG_DIR"]
	if dir == "" {
		dir = "/var/log/unattended-upgrades"
	}
	return service.run("tail", "-n", "60", "--", strings.TrimRight(dir, "/")+"/unattended-upgrades.log")
}

func (service manager) showEffective() error {
	output, err := service.query("apt-config", "dump")
	if err != nil {
		return err
	}
	ui.MenuSection("APT 生效配置")
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "APT::Periodic ") || strings.HasPrefix(line, "APT::Periodic::") ||
			strings.HasPrefix(line, "Unattended-Upgrade ") || strings.HasPrefix(line, "Unattended-Upgrade::") {
			fmt.Println(line)
		}
	}
	for _, name := range []string{refreshTimer, upgradeTimer} {
		fields, err := service.scheduleFields(name)
		if err != nil {
			fields = []ui.CardField{{Label: "读取失败", Value: err.Error()}}
		}
		fmt.Println()
		ui.PrintInfoCard(name, fields...)
	}
	return nil
}

func (service manager) runUpdate(view *ui.UI, simulate bool) error {
	if !service.root() {
		return fmt.Errorf("运行 unattended-upgrade 需要 root 权限")
	}
	if !service.exists("unattended-upgrade") {
		return fmt.Errorf("未安装 unattended-upgrades，请先安装")
	}
	if simulate {
		return service.run("unattended-upgrade", "--dry-run", "--verbose")
	}
	current, err := service.settings()
	if err != nil {
		return err
	}
	if current.shutdown == "true" {
		return fmt.Errorf("当前策略在关机时安装更新。要立即安装，请先关闭关机安装")
	}
	if err := confirm(view, "确认按生效策略立即更新？已开启的自动重启和软件包清理设置也会生效。(y/N)："); err != nil {
		return err
	}
	return service.run("unattended-upgrade", "--verbose")
}
