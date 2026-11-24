package ups

import (
	"fmt"
	"os"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

func ensureNUTInstalled(view *ui.UI) (bool, error) {
	if isNUTInstalled() {
		return true, nil
	}

	log.Warn("未检测到 NUT 服务端组件")
	ui.MenuSection("请选择安装方式")
	ui.MenuOptionHint("1", "手动安装", "显示安装命令并返回")
	ui.MenuOptionHint("2", "自动安装 NUT", "使用系统包管理器")
	ui.MenuExit("0/q", "取消")

	for {
		choice, err := view.Ask("请选择：")
		if err != nil {
			return false, err
		}
		fmt.Println()
		if shared.IsReturnChoice(choice) {
			return false, shared.ErrReturnToMenu
		}

		switch strings.ToLower(choice) {
		case "1":
			printNUTManualInstallHint()
			return false, nil
		case "2":
			if err := installNUT(); err != nil {
				return false, err
			}
			if !isNUTInstalled() {
				return false, fmt.Errorf("NUT 自动安装后仍未检测到服务端组件，请检查安装输出")
			}
			return true, nil
		default:
			ui.InvalidChoice()
		}
	}
}

func isNUTInstalled() bool {
	if system.DirExists(nutConfigDir) && commandAvailable("upsc", "/usr/bin/upsc") && nutServerComponentAvailable() {
		return true
	}
	return nutPackageInstalled()
}

func nutServerComponentAvailable() bool {
	return commandAvailable("upsd", "/usr/sbin/upsd", "/sbin/upsd") ||
		commandAvailable("usbhid-ups", "/lib/nut/usbhid-ups", "/usr/lib/nut/usbhid-ups", "/usr/libexec/nut/usbhid-ups")
}

func nutPackageInstalled() bool {
	switch {
	case system.CommandExists("dpkg-query"):
		out, err := system.Output("dpkg-query", "-W", "-f=${Status}", "nut")
		return err == nil && strings.Contains(out, "install ok installed")
	case system.CommandExists("rpm"):
		_, err := system.Output("rpm", "-q", "nut")
		return err == nil
	case system.CommandExists("pacman"):
		_, err := system.Output("pacman", "-Q", "nut")
		return err == nil
	default:
		return false
	}
}

func commandAvailable(name string, fallbackPaths ...string) bool {
	if system.CommandExists(name) {
		return true
	}
	for _, path := range fallbackPaths {
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

func printNUTManualInstallHint() {
	fmt.Println("请手动安装 NUT 后重新进入 UPS 配置。")
	fmt.Println()
	fmt.Println("常见命令：")
	switch {
	case system.CommandExists("apt-get"):
		fmt.Println("sudo apt update")
		fmt.Println("sudo apt install -y nut")
	case system.CommandExists("dnf"):
		fmt.Println("sudo dnf install -y nut")
	case system.CommandExists("yum"):
		fmt.Println("sudo yum install -y nut")
	case system.CommandExists("pacman"):
		fmt.Println("sudo pacman -Sy --noconfirm nut")
	case system.CommandExists("zypper"):
		fmt.Println("sudo zypper --non-interactive install nut")
	default:
		fmt.Println("请使用当前系统的包管理器安装 nut")
	}
}

func installNUT() error {
	log.Info("开始自动安装 NUT...")

	switch {
	case system.CommandExists("apt-get"):
		if err := system.Run("apt-get", "update"); err != nil {
			return fmt.Errorf("apt-get update 失败: %w", err)
		}
		return system.Run("apt-get", "install", "-y", "nut")
	case system.CommandExists("dnf"):
		return system.Run("dnf", "install", "-y", "nut")
	case system.CommandExists("yum"):
		return system.Run("yum", "install", "-y", "nut")
	case system.CommandExists("pacman"):
		return system.Run("pacman", "-Sy", "--noconfirm", "nut")
	case system.CommandExists("zypper"):
		return system.Run("zypper", "--non-interactive", "install", "nut")
	default:
		return fmt.Errorf("未识别支持的包管理器，请手动安装 NUT")
	}
}

func printLSUSBDevices() {
	fmt.Println(ui.PrimaryBoldText("当前 USB 设备列表："))
	fmt.Println(ui.MutedText("----------------------------------------"))
	if !system.CommandExists("lsusb") {
		log.Warn("未找到 lsusb 命令，已跳过 USB 设备列表")
		fmt.Println(ui.MutedText("----------------------------------------"))
		fmt.Println()
		return
	}

	if err := system.Run("lsusb"); err != nil {
		log.Warn("执行 lsusb 失败：", err)
	}
	fmt.Println(ui.MutedText("----------------------------------------"))
	fmt.Println()
}
