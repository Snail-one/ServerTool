package cleanup

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

type plan struct {
	name          string
	impact        string
	args          []string
	needsConfirm  bool
	confirmPrompt string
}

const runtimeName = "docker"

func Run(view *ui.UI, display string) error {
	log.Info("清理 ", display, " 容器与资源")
	for {
		ui.ClearScreen()
		ui.MenuTitle("容器管理", "Docker 清理")
		printDiskUsage(display, "当前 "+display+" 磁盘占用")
		fmt.Println()
		ui.PrintInfoCard("清理说明",
			ui.CardField{Label: "数据卷", Value: "选项 9 仅清理匿名卷；选项 10 包括命名卷；选项 7/8 保留卷"},
			ui.CardField{Label: "卷内数据", Value: "未使用不代表无用，删除前请确认备份"},
		)
		fmt.Println()
		for index := 1; index <= 10; index++ {
			key := strconv.Itoa(index)
			item, err := planForChoice(key)
			if err == nil {
				ui.MenuOptionHint(key, item.name, runtimeName+" "+strings.Join(item.args[:len(item.args)-1], " "))
			}
		}
		ui.MenuExit("0/q", "返回")

		choice, err := view.Ask("请选择：")
		if err != nil {
			return err
		}
		fmt.Println()
		if ui.IsReturnChoice(choice) {
			return ui.ErrReturnToMenu
		}

		item, err := planForChoice(choice)
		if err != nil {
			log.Warn(err)
			view.Pause()
			continue
		}

		ui.PrintWarningCard("容器清理影响",
			ui.CardField{Label: "操作", Value: item.name},
			ui.CardField{Label: "影响", Value: item.impact},
			ui.CardField{Label: "命令", Value: runtimeName + " " + strings.Join(item.args, " ")},
		)
		executed, err := execute(item, view.Confirm, system.Run, runtimeName)
		if err != nil {
			if errors.Is(err, ui.ErrReturnToMenu) {
				continue
			}
			if executed {
				return fmt.Errorf("%s 清理失败: %w", display, err)
			}
			return err
		}
		if !executed {
			fmt.Println("已取消容器清理")
			continue
		}
		fmt.Println()

		ui.PrintSuccessCard("容器资源清理完成",
			ui.CardField{Label: "运行时", Value: display},
			ui.CardField{Label: "操作", Value: item.name},
		)
		fmt.Println()
		printDiskUsage(display, "清理后 "+display+" 磁盘占用")
		return nil
	}
}

func execute(item plan, confirm func(string) (bool, error), run func(string, ...string) error, runtimeName string) (bool, error) {
	prompt := item.confirmPrompt
	if prompt == "" {
		prompt = "确认继续？(y/N)："
	}
	confirmed, err := confirm(prompt)
	if err != nil || !confirmed {
		return false, err
	}
	if err := run(runtimeName, item.args...); err != nil {
		return true, err
	}
	return true, nil
}

func planForChoice(choice string) (plan, error) {
	switch strings.ToLower(strings.TrimSpace(choice)) {
	case "1":
		return plan{
			name:         "清理悬空构建缓存",
			impact:       "永久删除悬空构建缓存，后续构建可能变慢；镜像、容器和卷不受影响。",
			args:         []string{"builder", "prune", "-f"},
			needsConfirm: true,
		}, nil
	case "2":
		return plan{
			name:         "清理全部未使用构建缓存",
			impact:       "永久删除所有未使用的构建缓存（包括非悬空缓存），后续构建可能变慢；镜像、容器和卷不受影响。",
			args:         []string{"builder", "prune", "-a", "-f"},
			needsConfirm: true,
		}, nil
	case "3":
		return plan{
			name:         "清理悬空镜像",
			impact:       "永久删除无标签且未使用的悬空镜像；卷不受影响。",
			args:         []string{"image", "prune", "-f"},
			needsConfirm: true,
		}, nil
	case "4":
		return plan{
			name:         "清理无用网络",
			impact:       "永久删除未被容器使用的网络；卷不受影响。",
			args:         []string{"network", "prune", "-f"},
			needsConfirm: true,
		}, nil
	case "5":
		return plan{
			name:         "清理所有未使用镜像",
			impact:       "永久删除所有未被容器使用的镜像，后续可能需要重新拉取；卷不受影响。",
			args:         []string{"image", "prune", "-a", "-f"},
			needsConfirm: true,
		}, nil
	case "6":
		return plan{
			name:         "清理停止容器",
			impact:       "永久删除所有已停止容器；运行中容器和卷不受影响。",
			args:         []string{"container", "prune", "-f"},
			needsConfirm: true,
		}, nil
	case "7":
		return plan{
			name:         "常规系统清理",
			impact:       "永久删除已停止容器、未使用网络、悬空镜像和构建缓存；卷不受影响。",
			args:         []string{"system", "prune", "-f"},
			needsConfirm: true,
		}, nil
	case "8":
		return plan{
			name:         "深度系统清理",
			impact:       "永久删除已停止容器、未使用网络、所有未使用镜像和构建缓存；卷不受影响。",
			args:         []string{"system", "prune", "-a", "-f"},
			needsConfirm: true,
		}, nil
	case "9":
		return plan{
			name:          "深度系统清理（含匿名卷）",
			impact:        "永久删除已停止容器、未使用网络、所有未使用镜像、构建缓存和未使用匿名卷及卷内数据；删除停止容器后失去引用的匿名卷也可能被删除。命名卷保留，匿名卷内数据无法恢复。",
			args:          []string{"system", "prune", "-a", "--volumes", "-f"},
			needsConfirm:  true,
			confirmPrompt: "确认已有备份并执行深度系统清理，永久删除未使用匿名数据卷及卷内数据？(y/N)：",
		}, nil
	case "10":
		return plan{
			name:          "清理所有未使用数据卷",
			impact:        "永久删除未被任何容器引用的数据卷（包括命名卷）及卷内全部数据；可能包含数据库和应用备份，无法恢复。被已停止容器引用的卷也会保留。",
			args:          []string{"volume", "prune", "-a", "-f"},
			needsConfirm:  true,
			confirmPrompt: "确认已有备份并永久删除所有未使用数据卷及卷内数据？(y/N)：",
		}, nil
	default:
		return plan{}, fmt.Errorf("无效容器清理选项: %s", choice)
	}
}
