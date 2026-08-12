package cleanup

import (
	"fmt"
	"strings"

	"snail_tool/internal/container/runtime"
	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

type dockerCleanupPlan struct {
	name         string
	impact       string
	args         []string
	needsConfirm bool
}

func Run(view *ui.UI) error {
	return CleanupDockerResources(view)
}

func CleanupDockerResources(view *ui.UI) error {
	rt, ok := runtime.Detect()
	if !ok {
		return fmt.Errorf("未检测到 Docker 或 Podman")
	}
	log.Info("清理 ", rt.Display, " 容器与资源")
	return runDockerCleanup(view, rt)
}

func runDockerCleanup(view *ui.UI, rt runtime.Runtime) error {
	for {
		ui.ClearScreen()
		ui.MenuTitle("容器管理", "清理容器资源")
		log.Info("当前 ", rt.Display, " 磁盘占用")
		printContainerDiskUsage(rt)
		fmt.Println()
		ui.MenuOptionHint("1", "清理停止容器", rt.Name+" container prune")
		ui.MenuOptionHint("2", "清理未使用网络", rt.Name+" network prune")
		ui.MenuOptionHint("3", "清理悬空镜像", rt.Name+" image prune")
		ui.MenuOptionHint("4", "清理构建缓存", rt.Name+" builder prune")
		ui.MenuOptionHint("5", "清理未使用镜像", rt.Name+" image prune -a")
		ui.MenuOptionHint("6", "常规系统清理", rt.Name+" system prune")
		ui.MenuOptionHint("7", "深度系统清理", rt.Name+" system prune -a")
		ui.MenuExit("0/q", "返回")
		fmt.Println()
		ui.PrintInfoCard("清理说明",
			ui.CardField{Label: "数据卷", Value: ui.Badge("不会删除", true)},
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

		plan, err := dockerCleanupPlanForChoice(choice)
		if err != nil {
			log.Warn(err)
			view.Pause()
			continue
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
	confirmed, err := confirm("确认继续？(y/N)：")
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
			name:         "清理停止容器",
			impact:       "永久删除所有已停止容器；运行中容器和卷不受影响。",
			args:         []string{"container", "prune", "-f"},
			needsConfirm: true,
		}, nil
	case "2":
		return dockerCleanupPlan{
			name:         "清理无用网络",
			impact:       "永久删除未被容器使用的网络；卷不受影响。",
			args:         []string{"network", "prune", "-f"},
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
			name:         "清理构建缓存",
			impact:       "永久删除未使用的构建缓存；镜像、容器和卷不受影响。",
			args:         []string{"builder", "prune", "-f"},
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
			name:         "清理容器无用资源",
			impact:       "永久删除已停止容器、未使用网络、悬空镜像和构建缓存；卷不受影响。",
			args:         []string{"system", "prune", "-f"},
			needsConfirm: true,
		}, nil
	case "7":
		return dockerCleanupPlan{
			name:         "深度清理容器无用资源",
			impact:       "永久删除已停止容器、未使用网络、所有未使用镜像和构建缓存；卷不受影响。",
			args:         []string{"system", "prune", "-a", "-f"},
			needsConfirm: true,
		}, nil
	default:
		return dockerCleanupPlan{}, fmt.Errorf("无效容器清理选项: %s", choice)
	}
}

func printContainerDiskUsage(runtime runtime.Runtime) {
	if err := system.Run(runtime.Name, "system", "df"); err != nil {
		log.Warn("无法获取 ", runtime.Display, " 磁盘占用：", err)
	}
}
