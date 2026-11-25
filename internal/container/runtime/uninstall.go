package runtime

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"snail_tool/internal/container/docker"
	"snail_tool/internal/container/podman"
	"snail_tool/internal/ui"
)

func UninstallRuntime(view *ui.UI, selected Runtime) (bool, error) {
	if selected.Name == "podman" {
		return podman.Uninstall(view)
	}
	return docker.Uninstall(view)
}

func Uninstall(view *ui.UI) (bool, error) {
	runtimes := DetectAll()
	if len(runtimes) == 0 {
		return false, fmt.Errorf("未检测到 Docker 或 Podman")
	}
	return uninstallDetectedRuntimes(view, runtimes, func(selected Runtime) (bool, error) {
		if selected.Name == "podman" {
			return podman.Uninstall(view)
		}
		return docker.Uninstall(view)
	})
}

func uninstallDetectedRuntimes(view interface {
	Ask(string) (string, error)
}, runtimes []Runtime, remove func(Runtime) (bool, error)) (bool, error) {
	for {
		selected := runtimes[0]
		if len(runtimes) > 1 {
			var proceed bool
			var err error
			selected, proceed, err = selectRuntimeToUninstall(view, runtimes)
			if err != nil || !proceed {
				return false, err
			}
		}
		removed, err := remove(selected)
		if len(runtimes) > 1 && (errors.Is(err, ui.ErrReturnToMenu) || err == nil && !removed) {
			continue
		}
		return removed, err
	}
}

func selectRuntimeToUninstall(view interface {
	Ask(string) (string, error)
}, runtimes []Runtime) (Runtime, bool, error) {
	for {
		ui.MenuSection("检测到多个容器运行时，请选择要卸载的运行时")
		for index, current := range runtimes {
			ui.MenuOption(strconv.Itoa(index+1), current.Display)
		}
		ui.MenuExit("0/q", "返回")
		choice, err := view.Ask("请选择：")
		if err != nil {
			return Runtime{}, false, err
		}
		if ui.IsReturnChoice(choice) {
			return Runtime{}, false, nil
		}
		switch strings.ToLower(strings.TrimSpace(choice)) {
		case "1":
			return runtimes[0], true, nil
		case "2":
			if len(runtimes) > 1 {
				return runtimes[1], true, nil
			}
		}
		ui.InvalidChoice()
		fmt.Println()
	}
}
