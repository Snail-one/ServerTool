package docker

import (
	"errors"
	"fmt"
	"strings"

	"snail_tool/internal/container/podman"
	"snail_tool/internal/log"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

func Manage(view *ui.UI) error {
	noted := false
	for {
		installed := probe(system.CommandExists, system.Output, nil)
		if !noted && len(installed) > 0 {
			displays := make([]string, 0, len(installed))
			for _, item := range installed {
				displays = append(displays, item.Display)
			}
			log.Info("已检测到容器运行时：", strings.Join(displays, "、"))
			noted = true
		}
		name, err := chooseRuntime(view, installed)
		if err != nil {
			return err
		}
		if selected, ok := findRuntime(installed, name); ok {
			err = runInstalled(view, selected, true)
			if errors.Is(err, ErrBackToRuntimes) || errors.Is(err, podman.ErrBackToRuntimes) {
				continue
			}
			return err
		}
		err = installMissing(view, name)
		if errors.Is(err, ui.ErrReturnToMenu) {
			continue
		}
		return err
	}
}

func installMissing(view *ui.UI, name string) error {
	if err := ensureAbsent(system.CommandExists, system.Output); err != nil {
		log.Error(err)
		view.Pause()
		return ui.ErrReturnToMenu
	}
	switch name {
	case "docker":
		return chooseDockerInstall(view)
	case "podman":
		return installPodman(view)
	default:
		return nil
	}
}

func chooseDockerInstall(view *ui.UI) error {
	invalidChoice := false
	for {
		ui.ClearScreen()
		ui.MenuTitle("容器管理", "安装 Docker")
		fmt.Println("未检测到 Docker。")
		fmt.Println()
		ui.MenuSection("请选择安装方式")
		ui.MenuOptionHint("1", "安装 Docker", "Docker 官方签名 stable 仓库")
		ui.MenuOptionHint("2", "安装 Docker", "Docker 官方脚本 get.docker.com")
		ui.MenuExit("0/q", "返回")

		if invalidChoice {
			ui.InvalidChoice()
			invalidChoice = false
		}
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
			return installFromRepository(view)
		case "2":
			return installFromScript(view)
		default:
			invalidChoice = true
		}
	}
}

func installFromRepository(view *ui.UI) error {
	if err := ensureAbsent(system.CommandExists, system.Output); err != nil {
		return err
	}
	if !system.IsRoot() {
		return fmt.Errorf("安装 Docker 需要 root 权限，请使用 sudo 运行本工具")
	}
	return Install(view)
}

func installFromScript(view *ui.UI) error {
	if err := ensureAbsent(system.CommandExists, system.Output); err != nil {
		return err
	}
	if !system.IsRoot() {
		return fmt.Errorf("使用官方脚本安装 Docker 需要 root 权限，请使用 sudo 运行本工具")
	}
	return InstallWithScript(view)
}

func installPodman(view *ui.UI) error {
	return podman.Install(view, func() error {
		return ensureAbsent(system.CommandExists, system.Output)
	})
}

func runInstalled(view *ui.UI, selected Runtime, backToChooser bool) error {
	current := func() (string, bool) {
		item, ok := findRuntime(probe(system.CommandExists, system.Output, nil), selected.Name)
		return item.Display, ok
	}
	if selected.Name == "podman" {
		return podman.Run(view, current, backToChooser)
	}
	return Run(view, current, backToChooser)
}

func chooseRuntime(view *ui.UI, installed []Runtime) (string, error) {
	for {
		ui.ClearScreen()
		ui.MenuTitle("容器管理")
		logSummary, proxySummary, clientProxySummary := "", "", ""
		if _, ok := findRuntime(installed, "docker"); ok {
			logSummary, proxySummary, clientProxySummary = ConfigSummaries()
		}
		ui.PrintInfoCard("容器运行时状态", runtimeStatusFields(installed, logSummary, proxySummary, clientProxySummary)...)
		fmt.Println()
		_, dockerInstalled := findRuntime(installed, "docker")
		_, podmanInstalled := findRuntime(installed, "podman")
		ui.MenuOptionStatus("1", "Docker", ui.InstallationBadge(dockerInstalled))
		ui.MenuOptionStatus("2", "Podman", ui.InstallationBadge(podmanInstalled))
		ui.MenuExit("0/q", "返回")

		choice, err := view.Ask("请选择：")
		if err != nil {
			return "", err
		}
		fmt.Println()
		if ui.IsReturnChoice(choice) {
			return "", ui.ErrReturnToMenu
		}
		if name, ok := runtimeByChoice(choice); ok {
			return name, nil
		}
		ui.InvalidChoice()
		view.Pause()
	}
}

func runtimeByChoice(choice string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(choice)) {
	case "1":
		return "docker", true
	case "2":
		return "podman", true
	default:
		return "", false
	}
}

func findRuntime(installed []Runtime, name string) (Runtime, bool) {
	for _, item := range installed {
		if item.Name == name {
			return item, true
		}
	}
	return Runtime{}, false
}

func runtimeStatusFields(installed []Runtime, logSummary, proxySummary, clientProxySummary string) []ui.CardField {
	fields := []ui.CardField{{
		Label: "当前运行时",
		Value: displaySummary(installed),
	}}
	if _, ok := findRuntime(installed, "docker"); ok {
		return append(fields, DetailFields(logSummary, proxySummary, clientProxySummary)...)
	}
	return fields
}

func displaySummary(runtimes []Runtime) string {
	if len(runtimes) == 0 {
		return "未安装"
	}
	displays := make([]string, 0, len(runtimes))
	for _, item := range runtimes {
		displays = append(displays, item.Display)
	}
	result := strings.Join(displays, "、")
	if len(runtimes) > 1 && runtimes[0].Name == "docker" {
		result += "（容器操作优先 Docker）"
	}
	return result
}
