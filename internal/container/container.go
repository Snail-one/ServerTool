package container

import (
	"errors"
	"fmt"
	"strings"

	containercleanup "snail_tool/internal/container/cleanup"
	"snail_tool/internal/container/compose"
	"snail_tool/internal/container/dockerconfig"
	"snail_tool/internal/container/dockerconfig/clientproxy"
	"snail_tool/internal/container/dockerconfig/daemonlog"
	"snail_tool/internal/container/dockerconfig/daemonproxy"
	"snail_tool/internal/container/list"
	"snail_tool/internal/container/runtime"
	"snail_tool/internal/ui"
)

var errBackToRuntimes = errors.New("back to container runtimes")

func Run(view *ui.UI) error {
	if err := runtime.Ensure(view); err != nil {
		return err
	}

	for {
		installed := runtime.DetectAll()
		if len(installed) == 0 {
			return ui.ErrReturnToMenu
		}
		if len(installed) == 1 {
			return runRuntimeMenu(view, installed[0].Name, false)
		}
		name, err := chooseContainerRuntime(view, installed)
		if err != nil {
			return err
		}
		err = runRuntimeMenu(view, name, true)
		if errors.Is(err, errBackToRuntimes) {
			continue
		}
		return err
	}
}

func chooseContainerRuntime(view *ui.UI, installed []runtime.Runtime) (string, error) {
	for {
		ui.ClearScreen()
		ui.MenuTitle("容器管理")
		logSummary, proxySummary, clientProxySummary := dockerConfigSummaries(installed)
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
		if selected, ok := containerRuntimeByChoice(installed, choice); ok {
			return selected.Name, nil
		}
		ui.InvalidChoice()
		view.Pause()
	}
}

func containerRuntimeByChoice(installed []runtime.Runtime, choice string) (runtime.Runtime, bool) {
	name := map[string]string{"1": "docker", "2": "podman"}[strings.ToLower(strings.TrimSpace(choice))]
	if name == "" {
		return runtime.Runtime{}, false
	}
	for _, item := range installed {
		if item.Name == name {
			return item, true
		}
	}
	return runtime.Runtime{}, false
}

func runRuntimeMenu(view *ui.UI, runtimeName string, backToChooser bool) error {
	for {
		installed := runtime.DetectAll()
		selected, ok := findRuntime(installed, runtimeName)
		if !ok {
			if backToChooser {
				return errBackToRuntimes
			}
			return ui.ErrReturnToMenu
		}
		labels := containerMenuLabels([]runtime.Runtime{selected})
		ui.ClearScreen()
		ui.MenuTitle("容器管理", runtimeMenuTitle(selected))
		logSummary, proxySummary, clientProxySummary := dockerConfigSummaries([]runtime.Runtime{selected})
		ui.PrintInfoCard("容器运行时状态", runtimeStatusFields([]runtime.Runtime{selected}, logSummary, proxySummary, clientProxySummary)...)
		fmt.Println()
		ui.MenuOptionHint("1", labels.manage, "查看状态、日志与生命周期操作")
		ui.MenuOptionHint("2", "Compose", "查看、更新与重建项目")
		fmt.Println()
		ui.MenuOptionHint("3", labels.config, labels.configHint)
		fmt.Println()
		ui.MenuOptionHint("4", labels.cleanup, "容器、网络、镜像、数据卷与构建缓存")
		ui.MenuOptionHint("5", labels.uninstall, "可选择保留或永久删除数据")
		ui.MenuExit("0/q", "返回")

		choice, err := view.Ask("请选择：")
		if err != nil {
			return err
		}
		fmt.Println()
		if ui.IsReturnChoice(choice) {
			if backToChooser {
				return errBackToRuntimes
			}
			return ui.ErrReturnToMenu
		}

		switch strings.ToLower(choice) {
		case "1":
			ui.RunAction(view, "容器列表与操作失败，已返回容器管理", func() error {
				return list.RunRuntime(view, selected)
			})
		case "2":
			ui.RunAction(view, "Compose 项目失败，已返回容器管理", func() error {
				return compose.RunRuntime(view, selected)
			})
		case "3":
			if !labels.configEnabled {
				fmt.Println("Podman 暂无这项配置")
				view.Pause()
				continue
			}
			ui.RunAction(view, "Docker 配置失败，已返回容器管理", func() error {
				return dockerconfig.Run(view)
			})
		case "4":
			ui.RunAction(view, "容器资源清理失败，已返回容器管理", func() error {
				return containercleanup.RunRuntime(view, selected)
			})
		case "5":
			uninstalled, err := runtime.UninstallRuntime(view, selected)
			if err != nil {
				if errors.Is(err, ui.ErrReturnToMenu) {
					continue
				}
				return err
			}
			if uninstalled {
				return ui.ErrReturnToMenu
			}
		default:
			ui.InvalidChoice()
			view.Pause()
		}
	}
}

func findRuntime(installed []runtime.Runtime, name string) (runtime.Runtime, bool) {
	for _, item := range installed {
		if item.Name == name {
			return item, true
		}
	}
	return runtime.Runtime{}, false
}

func runtimeMenuTitle(item runtime.Runtime) string {
	if item.Name == "podman" {
		return "Podman"
	}
	return "Docker"
}

func dockerConfigSummaries(installed []runtime.Runtime) (string, string, string) {
	for _, item := range installed {
		if item.Name == "docker" {
			return daemonlog.CurrentSummary(), daemonproxy.CurrentSummary(), clientproxy.CurrentSummary()
		}
	}
	return "", "", ""
}

type containerMenuLabelSet struct {
	manage        string
	config        string
	configHint    string
	cleanup       string
	uninstall     string
	configEnabled bool
}

func containerMenuLabels(installed []runtime.Runtime) containerMenuLabelSet {
	hasDocker, hasPodman := false, false
	for _, item := range installed {
		switch item.Name {
		case "docker":
			hasDocker = true
		case "podman":
			hasPodman = true
		}
	}
	if !hasDocker && hasPodman {
		return containerMenuLabelSet{
			manage:     "Podman 管理",
			config:     "Podman 配置",
			configHint: "暂无服务代理、构建代理和日志轮转",
			cleanup:    "Podman 清理",
			uninstall:  uninstallMenuLabel(installed),
		}
	}
	return containerMenuLabelSet{
		manage:        "Docker 管理",
		config:        "Docker 配置",
		configHint:    "服务代理、构建与容器代理、日志轮转",
		cleanup:       "Docker 清理",
		uninstall:     uninstallMenuLabel(installed),
		configEnabled: hasDocker,
	}
}

func uninstallMenuLabel(installed []runtime.Runtime) string {
	name := "容器运行时"
	if len(installed) == 1 {
		name = installed[0].Display
	} else if len(installed) > 1 {
		name = "Docker 或 Podman"
	}
	if name == "Docker" || strings.HasPrefix(name, "Docker（") {
		return "Docker 卸载" + strings.TrimPrefix(name, "Docker")
	}
	if name == "Podman" {
		return "Podman 卸载"
	}
	return "卸载 " + name
}

func runtimeStatusFields(installedRuntimes []runtime.Runtime, logSummary, proxySummary, clientProxySummary string) []ui.CardField {
	fields := []ui.CardField{{
		Label: "当前运行时",
		Value: runtime.DisplaySummary(installedRuntimes),
	}}
	for _, installed := range installedRuntimes {
		if installed.Name != "docker" {
			continue
		}
		return append(fields,
			ui.CardField{Label: "容器日志", Value: logSummary},
			ui.CardField{Label: "服务代理", Value: proxySummary},
			ui.CardField{Label: "构建与容器代理", Value: clientProxySummary},
		)
	}
	return fields
}
