package compose

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"snail_tool/internal/container/compose/update"
	"snail_tool/internal/container/internal/listing"
	"snail_tool/internal/container/runtime"
	"snail_tool/internal/input"
	"snail_tool/internal/log"
	"snail_tool/internal/ui"
)

type composeProjectAction struct {
	Name string
	Args []string
}

func manageRunning(view *ui.UI) error {
	rt, ok := runtime.Detect()
	if !ok {
		return fmt.Errorf("未检测到 Docker 或 Podman")
	}
	return manageComposeProjects(view, rt, true)
}

// manageScanned 入口：管理通过扫描目录获取的 docker compose 项目
func manageScanned(view *ui.UI) error {
	rt, ok := runtime.Detect()
	if !ok {
		return fmt.Errorf("未检测到 Docker 或 Podman")
	}
	return manageComposeProjects(view, rt, false)
}

func composeProjectStatusDisplay(project composeProjectInfo) string {
	if project.Status != "" {
		return project.Status
	}
	if project.StatusError != "" {
		return "状态获取失败"
	}
	if !project.Containers.HasData {
		return "未知"
	}
	if project.Containers.Total == 0 {
		return "无容器"
	}
	switch {
	case project.Containers.Running == project.Containers.Total:
		return "运行中"
	case project.Containers.Running > 0:
		return "部分运行"
	case project.Containers.Restarting > 0:
		return "重启中"
	case project.Containers.Paused > 0:
		return "已暂停"
	case project.Containers.Unknown > 0:
		return "未知"
	default:
		return "已停止"
	}
}

func composeContainerSummaryDisplay(summary composeContainerSummary) string {
	if !summary.HasData {
		return "-"
	}
	if summary.Total == 0 {
		return "无容器"
	}

	parts := []string{fmt.Sprintf("总%d", summary.Total)}
	if summary.Running > 0 {
		parts = append(parts, fmt.Sprintf("运行%d", summary.Running))
	}
	if summary.Exited > 0 {
		parts = append(parts, fmt.Sprintf("停止%d", summary.Exited))
	}
	if summary.Paused > 0 {
		parts = append(parts, fmt.Sprintf("暂停%d", summary.Paused))
	}
	if summary.Restarting > 0 {
		parts = append(parts, fmt.Sprintf("重启%d", summary.Restarting))
	}
	if summary.Created > 0 {
		parts = append(parts, fmt.Sprintf("创建%d", summary.Created))
	}
	if summary.Unknown > 0 {
		parts = append(parts, fmt.Sprintf("未知%d", summary.Unknown))
	}
	return strings.Join(parts, " ")
}

func printComposeProjects(projects []composeProjectInfo) {
	fmt.Println(ui.PrimaryBoldText("Compose 项目列表："))
	fmt.Println(ui.InfoText(fmt.Sprintf("%-4s %-16s %-28s %-24s %s", "编号", "项目状态", "容器状态", "项目名", "目录")))
	for i, project := range projects {
		projectStatus := colorComposeStatus(composeProjectStatusDisplay(project))
		fmt.Printf(
			"%-4s %s %-28s %-24s %s\n",
			fmt.Sprintf("%d)", i+1),
			ui.TableCell(projectStatus, 16),
			listing.TruncateText(listing.DefaultText(composeContainerSummaryDisplay(project.Containers)), 28),
			listing.TruncateText(listing.DefaultText(project.Name), 24),
			listing.DefaultText(project.Dir),
		)
	}
}

func colorComposeStatus(status string) string {
	switch status {
	case "运行中":
		return ui.StateText(status, true)
	case "部分运行", "重启中":
		return ui.InfoText(status)
	case "状态获取失败":
		return ui.DangerText(status)
	default:
		return ui.MutedText(status)
	}
}

func manageComposeProjects(view *ui.UI, rt runtime.Runtime, useLS bool) error {
	compose, err := update.DetectComposeCommandForRuntime(rt.Name)
	if err != nil {
		return err
	}

	for {
		var projects []composeProjectInfo
		if useLS {
			projects, err = listComposeProjectsFromLS(compose)
			if err != nil {
				return err
			}
		} else {
			var dirs []string
			_, dirs, err = update.AskComposeScanDirs(view, update.DefaultComposeRoots())
			if err != nil {
				return err
			}
			projects = listComposeProjectsFromDirs(compose, dirs)
		}

		if len(projects) == 0 {
			log.Info("未发现 Compose 项目")
			view.Pause()
			if !useLS {
				continue
			}
			return ui.ErrReturnToMenu
		}

		for {
			ui.ClearScreen()
			ui.MenuTitle("容器管理", "Compose 项目")
			printComposeProjects(projects)
			fmt.Println()

			raw, err := view.Ask("请选择项目编号（0/q 返回）：")
			if err != nil {
				return err
			}
			fmt.Println()

			if ui.IsReturnChoice(raw) {
				if !useLS {
					break
				}
				return ui.ErrReturnToMenu
			}

			idx, err := input.Number(raw, 1, len(projects))
			if err != nil || idx < 1 || idx > len(projects) {
				ui.InvalidChoice()
				view.Pause()
				continue
			}

			project := projects[idx-1]
			if err := manageSingleProject(view, compose, project.Dir); err != nil {
				if errors.Is(err, ui.ErrReturnToMenu) {
					continue
				}
				return err
			}
			// Keep the project selector after an operation.
			continue
		}
	}
}

func manageSingleProject(view *ui.UI, compose update.ComposeCommand, dir string) error {
	for {
		ui.ClearScreen()
		ui.MenuTitle("容器管理", "Compose 项目", filepath.Base(dir))
		fmt.Printf("项目目录：%s\n", dir)
		fmt.Println()
		summary, _, err := composeProjectContainerSummary(compose, dir, true)
		status := "状态获取失败"
		if err == nil {
			status = composeProjectStatusDisplay(composeProjectInfo{Containers: summary})
		}
		badge := ui.SoftwareBadge(status, summary.Running > 0)
		ui.MenuOptionStatusHint("1", "创建并后台启动", badge, compose.Display+" up -d")
		ui.MenuOptionStatusHint("2", "停止服务容器", badge, compose.Display+" stop")
		ui.MenuOptionHint("3", "重启服务容器", compose.Display+" restart")
		ui.MenuOptionHint("4", "删除 Compose 项目资源", compose.Display+" down")
		ui.MenuExit("0/q", "返回")

		raw, err := view.Ask("请选择：")
		if err != nil {
			return err
		}
		fmt.Println()

		if ui.IsReturnChoice(raw) {
			return ui.ErrReturnToMenu
		}

		action, ok := composeProjectActionForChoice(strings.ToLower(strings.TrimSpace(raw)))
		if !ok {
			ui.InvalidChoice()
			view.Pause()
			continue
		}

		prompt := fmt.Sprintf("项目目录：%s\n确认执行 %s %s？(y/N)：", dir, compose.Display, action.Name)
		if action.Name == "down" {
			prompt = fmt.Sprintf("项目目录：%s\n确认执行 %s down（不会删除卷）？(y/N)：", dir, compose.Display)
		}
		confirmed, err := view.Confirm(prompt)
		if err != nil {
			if errors.Is(err, ui.ErrReturnToMenu) {
				continue
			}
			return err
		}
		if !confirmed {
			fmt.Println("已取消")
			continue
		}

		if err := update.RunCompose(compose, dir, action.Args...); err != nil {
			log.Error("执行失败: ", err)
		} else {
			log.Info("已执行 compose ", action.Name, " 于 ", dir)
		}
		view.Pause()
		return nil
	}
}

func composeProjectActionForChoice(choice string) (composeProjectAction, bool) {
	switch choice {
	case "1":
		return composeProjectAction{Name: "up -d", Args: []string{"up", "-d"}}, true
	case "2":
		return composeProjectAction{Name: "stop", Args: []string{"stop"}}, true
	case "3":
		return composeProjectAction{Name: "restart", Args: []string{"restart"}}, true
	case "4":
		return composeProjectAction{Name: "down", Args: []string{"down"}}, true
	default:
		return composeProjectAction{}, false
	}
}
