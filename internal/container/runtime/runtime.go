package runtime

import (
	"fmt"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

type Runtime struct {
	Name    string
	Display string
}

func Ensure(view *ui.UI) error {
	for {
		runtime, ok := Detect()
		if ok {
			log.Info("已检测到容器运行时：", runtime.Display)
			return nil
		}

		ui.ClearScreen()
		fmt.Println("未检测到 Docker 或 Podman。")
		fmt.Println()
		fmt.Println("请选择安装方式：")
		fmt.Println("1) 安装 Docker（使用 https://get.docker.com 脚本）")
		fmt.Println("2) 安装 Podman（使用 apt 安装）")
		fmt.Println("3) 返回")
		fmt.Println()

		choice, err := view.Ask("输入选项: ")
		if err != nil {
			return err
		}
		fmt.Println()

		switch strings.ToLower(strings.TrimSpace(choice)) {
		case "1":
			if err := installDockerRuntime(); err != nil {
				return err
			}
		case "2":
			if err := installPodmanRuntime(); err != nil {
				return err
			}
		case "3", "0", "q", "exit":
			return shared.ErrReturnToMenu
		default:
			fmt.Println("无效选项，请重新输入")
			continue
		}
	}
}

func Detect() (Runtime, bool) {
	return runtimeForCommands(system.CommandExists("docker"), system.CommandExists("podman"))
}

func runtimeForCommands(hasDocker, hasPodman bool) (Runtime, bool) {
	switch {
	case hasDocker:
		return Runtime{Name: "docker", Display: "Docker"}, true
	case hasPodman:
		return Runtime{Name: "podman", Display: "Podman"}, true
	default:
		return Runtime{}, false
	}
}

func installDockerRuntime() error {
	if !system.IsRoot() {
		return fmt.Errorf("安装 Docker 需要 root 权限，请使用 sudo 运行本工具")
	}

	log.Info("安装 Docker...")
	switch {
	case system.CommandExists("curl"):
		return system.Run("sh", "-c", "curl -fsSL https://get.docker.com | sh")
	case system.CommandExists("wget"):
		return system.Run("sh", "-c", "wget -qO- https://get.docker.com | sh")
	default:
		return fmt.Errorf("未找到 curl 或 wget，无法下载 https://get.docker.com 安装脚本")
	}
}

func installPodmanRuntime() error {
	if !system.IsRoot() {
		return fmt.Errorf("安装 Podman 需要 root 权限，请使用 sudo 运行本工具")
	}

	log.Info("安装 Podman...")
	switch {
	case system.CommandExists("apt"):
		if err := system.Run("apt", "update"); err != nil {
			return fmt.Errorf("apt update 失败: %w", err)
		}
		return system.Run("apt", "install", "-y", "podman")
	case system.CommandExists("apt-get"):
		if err := system.Run("apt-get", "update"); err != nil {
			return fmt.Errorf("apt-get update 失败: %w", err)
		}
		return system.Run("apt-get", "install", "-y", "podman")
	default:
		return fmt.Errorf("未找到 apt 或 apt-get，无法自动安装 Podman")
	}
}
