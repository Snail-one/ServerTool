package runtime

import (
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
	selected := runtimes[0]
	if len(runtimes) > 1 {
		var proceed bool
		var err error
		selected, proceed, err = selectRuntimeToUninstall(view, runtimes)
		if err != nil || !proceed {
			return false, err
		}
	}
	if selected.Name == "podman" {
		return UninstallPodman(view)
	}
	return UninstallDocker(view)
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
