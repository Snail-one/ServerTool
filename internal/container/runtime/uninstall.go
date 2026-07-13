package runtime

import (
	"fmt"
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
		fmt.Println("检测到多个容器运行时，请选择要卸载的运行时：")
		for index, current := range runtimes {
			fmt.Printf("%d) %s\n", index+1, current.Display)
		}
		fmt.Println("0/q) 返回")
		fmt.Println()
		choice, err := view.Ask("输入选项: ")
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
		fmt.Println("无效选项，请重新输入")
		fmt.Println()
	}
}
