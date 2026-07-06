package cleanup

import (
	"fmt"
	"strings"

	"snail_tool/internal/container/runtime"
	"snail_tool/internal/log"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

type dockerCleanupPlan struct {
	name         string
	args         []string
	needsConfirm bool
	skip         bool
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
	fmt.Println()
	log.Info("当前 ", rt.Display, " 磁盘占用")
	printContainerDiskUsage(rt)
	fmt.Println()
	fmt.Println("请选择容器清理操作：")
	fmt.Println("1) 一键清理无用资源（默认，停止容器、无用网络、悬空镜像和构建缓存）")
	fmt.Printf("2) 只清理停止容器（%s container prune -f）\n", rt.Name)
	fmt.Printf("3) 只清理无用网络（%s network prune -f）\n", rt.Name)
	fmt.Printf("4) 只清理悬空镜像（%s image prune -f）\n", rt.Name)
	fmt.Printf("5) 清理所有未被容器使用的镜像（%s image prune -a -f）\n", rt.Name)
	fmt.Printf("6) 只清理构建缓存（%s builder prune -f）\n", rt.Name)
	fmt.Println("7) 深度一键清理：停止容器、无用网络、所有未使用镜像和构建缓存")
	fmt.Println("0/q) 返回")
	fmt.Println()
	fmt.Println("说明：以上选项都不会清理 volume。")
	fmt.Println()

	choice, err := view.Ask("输入选项（直接回车默认 1）: ")
	if err != nil {
		return err
	}
	fmt.Println()

	plan, err := dockerCleanupPlanForChoice(choice)
	if err != nil {
		fmt.Println(err)
		fmt.Println("已返回容器管理")
		return nil
	}
	if plan.skip {
		fmt.Println("已返回容器管理")
		return nil
	}

	if plan.needsConfirm {
		confirmed, err := view.Confirm(fmt.Sprintf("%s 可能需要后续重新拉取镜像，确认继续？(y/N): ", plan.name))
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Println("已取消容器清理")
			return nil
		}
		fmt.Println()
	}

	log.Info(plan.name)
	if err := system.Run(rt.Name, plan.args...); err != nil {
		return fmt.Errorf("%s 清理失败: %w", rt.Display, err)
	}

	fmt.Println()
	log.Info("清理后 ", rt.Display, " 磁盘占用")
	printContainerDiskUsage(rt)
	return nil
}

func dockerCleanupPlanForChoice(choice string) (dockerCleanupPlan, error) {
	switch strings.ToLower(strings.TrimSpace(choice)) {
	case "", "1":
		return dockerCleanupPlan{
			name: "一键清理容器无用资源",
			args: []string{"system", "prune", "-f"},
		}, nil
	case "2":
		return dockerCleanupPlan{
			name: "清理停止容器",
			args: []string{"container", "prune", "-f"},
		}, nil
	case "3":
		return dockerCleanupPlan{
			name: "清理无用网络",
			args: []string{"network", "prune", "-f"},
		}, nil
	case "4":
		return dockerCleanupPlan{
			name: "清理悬空镜像",
			args: []string{"image", "prune", "-f"},
		}, nil
	case "5":
		return dockerCleanupPlan{
			name:         "清理所有未被容器使用的镜像",
			args:         []string{"image", "prune", "-a", "-f"},
			needsConfirm: true,
		}, nil
	case "6":
		return dockerCleanupPlan{
			name: "清理构建缓存",
			args: []string{"builder", "prune", "-f"},
		}, nil
	case "7":
		return dockerCleanupPlan{
			name:         "深度清理容器无用资源",
			args:         []string{"system", "prune", "-a", "-f"},
			needsConfirm: true,
		}, nil
	case "0", "q", "exit":
		return dockerCleanupPlan{skip: true}, nil
	default:
		return dockerCleanupPlan{}, fmt.Errorf("无效容器清理选项: %s", choice)
	}
}

func printContainerDiskUsage(runtime runtime.Runtime) {
	if err := system.Run(runtime.Name, "system", "df"); err != nil {
		log.Warn("无法获取 ", runtime.Display, " 磁盘占用：", err)
	}
}
