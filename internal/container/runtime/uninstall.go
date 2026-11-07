package runtime

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

func Uninstall(view *ui.UI) (bool, error) {
	runtimes := DetectAll()
	if len(runtimes) == 0 {
		return false, fmt.Errorf("未检测到 Docker 或 Podman")
	}
	return uninstallDetectedRuntimes(view, runtimes, func(selected Runtime) (bool, error) {
		if selected.Name == "podman" {
			return UninstallPodman(view)
		}
		return UninstallDocker(view)
	})
}

func uninstallDetectedRuntimes(view dockerUninstallPrompter, runtimes []Runtime, remove func(Runtime) (bool, error)) (bool, error) {
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
		if len(runtimes) > 1 && (errors.Is(err, shared.ErrReturnToMenu) || err == nil && !removed) {
			continue
		}
		return removed, err
	}
}

func runUninstallMode(view dockerUninstallPrompter, choose func(dockerUninstallPrompter) (bool, bool, error), remove func(bool) (bool, error)) (bool, error) {
	for {
		removeData, proceed, err := choose(view)
		if err != nil || !proceed {
			return false, err
		}
		removed, err := remove(removeData)
		if errors.Is(err, shared.ErrReturnToMenu) || err == nil && !removed {
			continue
		}
		return removed, err
	}
}

func selectRuntimeToUninstall(view dockerUninstallPrompter, runtimes []Runtime) (Runtime, bool, error) {
	for {
		ui.MenuSection("检测到多个容器运行时，请选择要卸载的运行时")
		for index, current := range runtimes {
			ui.MenuOption(strconv.Itoa(index+1), current.Display)
		}
		ui.MenuExit("0/q", "返回")
		fmt.Println()
		choice, err := view.Ask("请选择：")
		if err != nil {
			return Runtime{}, false, err
		}
		if shared.IsReturnChoice(choice) {
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
