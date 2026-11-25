package container

import (
	"errors"
	"fmt"
	"strings"

	"snail_tool/internal/container/docker"
	"snail_tool/internal/container/podman"
	"snail_tool/internal/log"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

func Manage(view *ui.UI) error {
	return manage(view, func() []Runtime { return DetectAllWithProgress(nil) }, runInstalled, installMissing)
}

func manage(view *ui.UI, detect func() []Runtime, run func(*ui.UI, Runtime) error, install func(*ui.UI, string) error) error {
	noted := false
	for {
		installed := detect()
		if !noted && len(installed) > 0 {
			log.Info("已检测到容器运行时：", RuntimeSummary(installed))
			noted = true
		}
		name, err := chooseRuntime(view, installed)
		if err != nil {
			return err
		}
		if selected, ok := findRuntime(installed, name); ok {
			err = run(view, selected)
			if errors.Is(err, docker.ErrBackToRuntimes) || errors.Is(err, podman.ErrBackToRuntimes) {
				continue
			}
			return err
		}
		ui.RunAction(view, "容器运行时安装失败，已返回容器管理", func() error {
			return install(view, name)
		})
	}
}

func installMissing(view *ui.UI, name string) error {
	check := func() error { return ensureAbsent(system.CommandExists, system.Output) }
	if err := check(); err != nil {
		return err
	}
	switch name {
	case "docker":
		return docker.ChooseInstall(view, check)
	case "podman":
		return podman.Install(view, check)
	default:
		return nil
	}
}

func runInstalled(view *ui.UI, selected Runtime) error {
	current := func() (string, bool) {
		item, ok := findRuntime(probe(system.CommandExists, system.Output, nil), selected.Name)
		return item.Display, ok
	}
	if selected.Name == "podman" {
		return podman.Run(view, current, true)
	}
	return docker.Run(view, current, true)
}

func chooseRuntime(view *ui.UI, installed []Runtime) (string, error) {
	for {
		ui.ClearScreen()
		ui.MenuTitle("容器管理")
		logSummary, proxySummary, clientProxySummary := "", "", ""
		if _, ok := findRuntime(installed, "docker"); ok {
			logSummary, proxySummary, clientProxySummary = docker.ConfigSummaries()
		}
		ui.PrintInfoCard("容器运行时状态", runtimeStatusFields(installed, logSummary, proxySummary, clientProxySummary)...)
		fmt.Println()
		_, dockerInstalled := findRuntime(installed, "docker")
		_, podmanInstalled := findRuntime(installed, "podman")
		showRuntimeOption("1", "Docker", dockerInstalled, len(installed) > 0)
		showRuntimeOption("2", "Podman", podmanInstalled, len(installed) > 0)
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
			if _, present := findRuntime(installed, name); !present {
				if err := checkInstallAllowed(installed); err != nil {
					log.Info(err)
					view.Pause()
					continue
				}
			}
			return name, nil
		}
		ui.InvalidChoice()
		view.Pause()
	}
}

func showRuntimeOption(key, name string, installed, hasRuntime bool) {
	badge := ui.InstallationBadge(installed)
	if !installed && hasRuntime {
		ui.MenuOptionStatusHint(key, name, badge, "安装前需先卸载当前运行时")
		return
	}
	ui.MenuOptionStatus(key, name, badge)
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
		Value: RuntimeSummary(installed),
	}}
	if _, ok := findRuntime(installed, "docker"); ok {
		return append(fields, docker.DetailFields(logSummary, proxySummary, clientProxySummary)...)
	}
	return fields
}

func RuntimeSummary(runtimes []Runtime) string {
	if len(runtimes) == 0 {
		return "未安装"
	}
	displays := make([]string, 0, len(runtimes))
	for _, item := range runtimes {
		displays = append(displays, item.Display)
	}
	return strings.Join(displays, "、")
}
