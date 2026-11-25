package container

import (
	"errors"
	"fmt"
	"strings"

	"snail_tool/internal/container/docker"
	dockercleanup "snail_tool/internal/container/docker/cleanup"
	dockercompose "snail_tool/internal/container/docker/compose"
	"snail_tool/internal/container/docker/dockerconfig"
	"snail_tool/internal/container/docker/dockerconfig/clientproxy"
	"snail_tool/internal/container/docker/dockerconfig/daemonlog"
	"snail_tool/internal/container/docker/dockerconfig/daemonproxy"
	dockerlist "snail_tool/internal/container/docker/list"
	"snail_tool/internal/container/podman"
	podmancleanup "snail_tool/internal/container/podman/cleanup"
	podmancompose "snail_tool/internal/container/podman/compose"
	podmanlist "snail_tool/internal/container/podman/list"
	"snail_tool/internal/ui"
)

var errBackToRuntimes = errors.New("back to container runtimes")

func Run(view *ui.UI) error {
	if err := Ensure(view); err != nil {
		return err
	}

	for {
		installed := DetectAll()
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

func chooseContainerRuntime(view *ui.UI, installed []Runtime) (string, error) {
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

func containerRuntimeByChoice(installed []Runtime, choice string) (Runtime, bool) {
	name := map[string]string{"1": "docker", "2": "podman"}[strings.ToLower(strings.TrimSpace(choice))]
	if name == "" {
		return Runtime{}, false
	}
	for _, item := range installed {
		if item.Name == name {
			return item, true
		}
	}
	return Runtime{}, false
}

func runRuntimeMenu(view *ui.UI, runtimeName string, backToChooser bool) error {
	for {
		installed := DetectAll()
		selected, ok := findRuntime(installed, runtimeName)
		if !ok {
			if backToChooser {
				return errBackToRuntimes
			}
			return ui.ErrReturnToMenu
		}
		labels := containerMenuLabels([]Runtime{selected})
		ui.ClearScreen()
		ui.MenuTitle("容器管理", runtimeMenuTitle(selected))
		logSummary, proxySummary, clientProxySummary := dockerConfigSummaries([]Runtime{selected})
		ui.PrintInfoCard("容器运行时状态", runtimeStatusFields([]Runtime{selected}, logSummary, proxySummary, clientProxySummary)...)
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
				if selected.Name == "podman" {
					return podmanlist.Run(view)
				}
				return dockerlist.Run(view)
			})
		case "2":
			ui.RunAction(view, "Compose 项目失败，已返回容器管理", func() error {
				if selected.Name == "podman" {
					return podmancompose.Run(view)
				}
				return dockercompose.Run(view)
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
				if selected.Name == "podman" {
					return podmancleanup.Run(view, selected.Display)
				}
				return dockercleanup.Run(view, selected.Display)
			})
		case "5":
			var uninstalled bool
			if selected.Name == "podman" {
				uninstalled, err = podman.Uninstall(view)
			} else {
				uninstalled, err = docker.Uninstall(view)
			}
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

func findRuntime(installed []Runtime, name string) (Runtime, bool) {
	for _, item := range installed {
		if item.Name == name {
			return item, true
		}
	}
	return Runtime{}, false
}

func runtimeMenuTitle(item Runtime) string {
	if item.Name == "podman" {
		return "Podman"
	}
	return "Docker"
}

func dockerConfigSummaries(installed []Runtime) (string, string, string) {
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

func containerMenuLabels(installed []Runtime) containerMenuLabelSet {
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

func uninstallMenuLabel(installed []Runtime) string {
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

func runtimeStatusFields(installedRuntimes []Runtime, logSummary, proxySummary, clientProxySummary string) []ui.CardField {
	fields := []ui.CardField{{
		Label: "当前运行时",
		Value: DisplaySummary(installedRuntimes),
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
