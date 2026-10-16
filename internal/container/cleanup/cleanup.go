package cleanup

import (
	"fmt"
	"strconv"
	"strings"

	"snail_tool/internal/container/runtime"
	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

type dockerCleanupPlan struct {
	name          string
	impact        string
	args          []string
	needsConfirm  bool
	confirmPrompt string
}

func Run(view *ui.UI) error {
	return CleanupDockerResources(view)
}

func CleanupDockerResources(view *ui.UI) error {
	runtimes := runtime.DetectAll()
	if len(runtimes) == 0 {
		return fmt.Errorf("未检测到 Docker 或 Podman")
	}
	rt, err := selectCleanupRuntime(view, runtimes)
	if err != nil {
		return err
	}
	log.Info("清理 ", rt.Display, " 容器与资源")
	return runDockerCleanup(view, rt)
}

func selectCleanupRuntime(view interface{ Ask(string) (string, error) }, runtimes []runtime.Runtime) (runtime.Runtime, error) {
	if len(runtimes) == 1 {
		return runtimes[0], nil
	}
	for {
		ui.MenuTitle("容器管理", "选择清理运行时")
		for index, rt := range runtimes {
			ui.MenuOption(strconv.Itoa(index+1), rt.Display)
		}
		ui.MenuExit("0/q", "返回")
		choice, err := view.Ask("请选择：")
		if err != nil {
			return runtime.Runtime{}, err
		}
		if shared.IsReturnChoice(choice) {
			return runtime.Runtime{}, shared.ErrReturnToMenu
		}
		index, err := strconv.Atoi(strings.TrimSpace(choice))
		if err == nil && index > 0 && index <= len(runtimes) {
			return runtimes[index-1], nil
		}
		ui.InvalidChoice()
	}
}

func runDockerCleanup(view *ui.UI, rt runtime.Runtime) error {
	for {
		ui.ClearScreen()
		ui.MenuTitle("容器管理", "清理容器资源")
		log.Info("当前 ", rt.Display, " 磁盘占用")
		printContainerDiskUsage(rt)
		fmt.Println()
		for index := 1; index <= 10; index++ {
			key := strconv.Itoa(index)
			plan, err := cleanupPlanForRuntime(key, rt.Name)
			if err == nil {
				ui.MenuOptionHint(key, plan.name, rt.Name+" "+strings.Join(plan.args[:len(plan.args)-1], " "))
			}
		}
		ui.MenuExit("0/q", "返回")
		fmt.Println()
		volumeNote := "仅选项 10 删除；常规与深度系统清理均保留"
		if rt.Name == "docker" {
			volumeNote = "选项 9 仅清理匿名卷；选项 10 包括命名卷；选项 7/8 保留卷"
		}
		ui.PrintInfoCard("清理说明",
			ui.CardField{Label: "数据卷", Value: volumeNote},
			ui.CardField{Label: "卷内数据", Value: "未使用不代表无用，删除前请确认备份"},
		)
		fmt.Println()

		choice, err := view.Ask("请选择：")
		if err != nil {
			return err
		}
		fmt.Println()
		if shared.IsReturnChoice(choice) {
			return shared.ErrReturnToMenu
		}

		plan, err := cleanupPlanForRuntime(choice, rt.Name)
		if err != nil {
			log.Warn(err)
			view.Pause()
			continue
		}
		if rt.Name == "podman" && strings.TrimSpace(choice) == "10" {
			plan, err = resolvePodmanVolumePlan(plan, system.Output)
			if err != nil {
				log.Warn(err)
				view.Pause()
				continue
			}
		}

		ui.PrintWarningCard("容器清理影响",
			ui.CardField{Label: "操作", Value: plan.name},
			ui.CardField{Label: "影响", Value: plan.impact},
			ui.CardField{Label: "命令", Value: rt.Name + " " + strings.Join(plan.args, " ")},
		)
		executed, err := executeDockerCleanupPlan(plan, view.Confirm, system.Run, rt.Name)
		if err != nil {
			if executed {
				return fmt.Errorf("%s 清理失败: %w", rt.Display, err)
			}
			return err
		}
		if !executed {
			fmt.Println("已取消容器清理")
			view.Pause()
			continue
		}
		fmt.Println()

		ui.PrintSuccessCard("容器资源清理完成",
			ui.CardField{Label: "运行时", Value: rt.Display},
			ui.CardField{Label: "操作", Value: plan.name},
		)
		fmt.Println()
		fmt.Println(ui.PrimaryBoldText("清理后 " + rt.Display + " 磁盘占用："))
		printContainerDiskUsage(rt)
		return nil
	}
}

func executeDockerCleanupPlan(plan dockerCleanupPlan, confirm func(string) (bool, error), run func(string, ...string) error, runtimeName string) (bool, error) {
	prompt := plan.confirmPrompt
	if prompt == "" {
		prompt = "确认继续？(y/N)："
	}
	confirmed, err := confirm(prompt)
	if err != nil || !confirmed {
		return false, err
	}
	if err := run(runtimeName, plan.args...); err != nil {
		return true, err
	}
	return true, nil
}

func dockerCleanupPlanForChoice(choice string) (dockerCleanupPlan, error) {
	switch strings.ToLower(strings.TrimSpace(choice)) {
	case "1":
		return dockerCleanupPlan{
			name:         "清理悬空构建缓存",
			impact:       "永久删除悬空构建缓存，后续构建可能变慢；镜像、容器和卷不受影响。",
			args:         []string{"builder", "prune", "-f"},
			needsConfirm: true,
		}, nil
	case "2":
		return dockerCleanupPlan{
			name:         "清理全部未使用构建缓存",
			impact:       "永久删除所有未使用的构建缓存（包括非悬空缓存），后续构建可能变慢；镜像、容器和卷不受影响。",
			args:         []string{"builder", "prune", "-a", "-f"},
			needsConfirm: true,
		}, nil
	case "3":
		return dockerCleanupPlan{
			name:         "清理悬空镜像",
			impact:       "永久删除无标签且未使用的悬空镜像；卷不受影响。",
			args:         []string{"image", "prune", "-f"},
			needsConfirm: true,
		}, nil
	case "4":
		return dockerCleanupPlan{
			name:         "清理无用网络",
			impact:       "永久删除未被容器使用的网络；卷不受影响。",
			args:         []string{"network", "prune", "-f"},
			needsConfirm: true,
		}, nil
	case "5":
		return dockerCleanupPlan{
			name:         "清理所有未使用镜像",
			impact:       "永久删除所有未被容器使用的镜像，后续可能需要重新拉取；卷不受影响。",
			args:         []string{"image", "prune", "-a", "-f"},
			needsConfirm: true,
		}, nil
	case "6":
		return dockerCleanupPlan{
			name:         "清理停止容器",
			impact:       "永久删除所有已停止容器；运行中容器和卷不受影响。",
			args:         []string{"container", "prune", "-f"},
			needsConfirm: true,
		}, nil
	case "7":
		return dockerCleanupPlan{
			name:         "常规系统清理",
			impact:       "永久删除已停止容器、未使用网络、悬空镜像和构建缓存；卷不受影响。",
			args:         []string{"system", "prune", "-f"},
			needsConfirm: true,
		}, nil
	case "8":
		return dockerCleanupPlan{
			name:         "深度系统清理",
			impact:       "永久删除已停止容器、未使用网络、所有未使用镜像和构建缓存；卷不受影响。",
			args:         []string{"system", "prune", "-a", "-f"},
			needsConfirm: true,
		}, nil
	case "9":
		return dockerCleanupPlan{
			name:          "深度系统清理（含匿名卷）",
			impact:        "永久删除已停止容器、未使用网络、所有未使用镜像、构建缓存和未使用匿名卷及卷内数据；删除停止容器后失去引用的匿名卷也可能被删除。命名卷保留，匿名卷内数据无法恢复。",
			args:          []string{"system", "prune", "-a", "--volumes", "-f"},
			needsConfirm:  true,
			confirmPrompt: "确认已有备份并执行深度系统清理，永久删除未使用匿名数据卷及卷内数据？(y/N)：",
		}, nil
	case "10":
		return dockerCleanupPlan{
			name:          "清理所有未使用数据卷",
			impact:        "永久删除未被任何容器引用的数据卷（包括命名卷）及卷内全部数据；可能包含数据库和应用备份，无法恢复。被已停止容器引用的卷也会保留。",
			args:          []string{"volume", "prune", "-a", "-f"},
			needsConfirm:  true,
			confirmPrompt: "确认已有备份并永久删除所有未使用数据卷及卷内数据？(y/N)：",
		}, nil
	default:
		return dockerCleanupPlan{}, fmt.Errorf("无效容器清理选项: %s", choice)
	}
}

func cleanupPlanForRuntime(choice, runtimeName string) (dockerCleanupPlan, error) {
	choice = strings.ToLower(strings.TrimSpace(choice))
	if runtimeName == "podman" && (choice == "1" || choice == "2") {
		return dockerCleanupPlan{}, fmt.Errorf("Podman 不支持 Docker builder prune，请使用系统清理")
	}
	if runtimeName == "podman" && choice == "9" {
		return dockerCleanupPlan{}, fmt.Errorf("含匿名卷的深度系统清理仅 Docker 可用；Podman 请使用系统清理或单独清理数据卷")
	}
	plan, err := dockerCleanupPlanForChoice(choice)
	if err != nil {
		return dockerCleanupPlan{}, err
	}
	if runtimeName == "podman" && choice == "10" {
		plan.args = []string{"volume", "prune", "-f"}
	}
	if runtimeName == "podman" && (choice == "7" || choice == "8") {
		plan.impact = "永久删除已停止容器、未使用的 Pod 和网络、悬空镜像和构建缓存；卷不受影响。"
		if choice == "8" {
			plan.impact = "永久删除已停止容器、未使用的 Pod 和网络、所有未使用镜像和构建缓存；卷不受影响。"
		}
	}
	return plan, nil
}

// Older Podman releases prune named volumes by default and do not accept --all.
func resolvePodmanVolumePlan(plan dockerCleanupPlan, output func(string, ...string) (string, error)) (dockerCleanupPlan, error) {
	help, err := output("podman", "volume", "prune", "--help")
	if err != nil {
		return dockerCleanupPlan{}, fmt.Errorf("无法确认 Podman 数据卷清理参数: %w", err)
	}
	plan.args = []string{"volume", "prune", "-f"}
	if strings.Contains(help, "--all") {
		plan.args = []string{"volume", "prune", "-a", "-f"}
	}
	return plan, nil
}

func printContainerDiskUsage(runtime runtime.Runtime) {
	if err := system.Run(runtime.Name, "system", "df"); err != nil {
		log.Warn("无法获取 ", runtime.Display, " 磁盘占用：", err)
	}
}
