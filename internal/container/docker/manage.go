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
	if err := ensure(view); err != nil {
		return err
	}

	for {
		installed := probe(system.CommandExists, system.Output, nil)
		if len(installed) == 0 {
			return ui.ErrReturnToMenu
		}
		if len(installed) == 1 {
			return runInstalled(view, installed[0], false)
		}
		name, err := chooseRuntime(view, installed)
		if err != nil {
			return err
		}
		selected, ok := findRuntime(installed, name)
		if !ok {
			continue
		}
		err = runInstalled(view, selected, true)
		if errors.Is(err, ErrBackToRuntimes) || errors.Is(err, podman.ErrBackToRuntimes) {
			continue
		}
		return err
	}
}

func ensure(view *ui.UI) error {
	invalidChoice := false
	for {
		runtimes := probe(system.CommandExists, system.Output, nil)
		if len(runtimes) > 0 {
			displays := make([]string, 0, len(runtimes))
			for _, item := range runtimes {
				displays = append(displays, item.Display)
			}
			log.Info("已检测到容器运行时：", strings.Join(displays, "、"))
			return nil
		}

		ui.ClearScreen()
		ui.MenuTitle("容器管理", "安装容器运行时")
		fmt.Println("未检测到 Docker 或 Podman。")
		fmt.Println()
		ui.MenuSection("请选择安装方式")
		ui.MenuOptionHint("1", "安装 Docker", "Docker 官方签名 stable 仓库")
		ui.MenuOptionHint("2", "安装 Docker", "Docker 官方脚本 get.docker.com")
		ui.MenuOptionHint("3", "安装 Podman", "apt 软件包")
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
			if err := installFromRepository(view); err != nil {
				if errors.Is(err, ui.ErrReturnToMenu) {
					continue
				}
				return err
			}
		case "2":
			if err := installFromScript(view); err != nil {
				if errors.Is(err, ui.ErrReturnToMenu) {
					continue
				}
				return err
			}
		case "3":
			if err := installPodman(view); err != nil {
				if errors.Is(err, ui.ErrReturnToMenu) {
					continue
				}
				return err
			}
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
		ui.MenuOption("1", "Docker")
		ui.MenuOption("2", "Podman")
		ui.MenuExit("0/q", "返回")

		choice, err := view.Ask("请选择：")
		if err != nil {
			return "", err
		}
		fmt.Println()
		if ui.IsReturnChoice(choice) {
			return "", ui.ErrReturnToMenu
		}
		if selected, ok := runtimeByChoice(installed, choice); ok {
			return selected.Name, nil
		}
		ui.InvalidChoice()
		view.Pause()
	}
}

func runtimeByChoice(installed []Runtime, choice string) (Runtime, bool) {
	name := map[string]string{"1": "docker", "2": "podman"}[strings.ToLower(strings.TrimSpace(choice))]
	if name == "" {
		return Runtime{}, false
	}
	return findRuntime(installed, name)
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
