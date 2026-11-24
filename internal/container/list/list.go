package list

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	composeprojects "snail_tool/internal/container/compose"
	"snail_tool/internal/container/compose/update"
	"snail_tool/internal/container/internal/listing"
	"snail_tool/internal/container/runtime"
	"snail_tool/internal/input"
	"snail_tool/internal/log"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

type containerAction struct {
	Key     string
	Label   string
	Command string
}

func Run(view *ui.UI) error {
	// 查看容器入口：直接列出并管理容器（含未启动）
	rt, ok := runtime.Detect()
	if !ok {
		return fmt.Errorf("未检测到 Docker 或 Podman")
	}
	return manageContainers(view, rt)
}

// ManageComposeLS 入口：管理通过 docker compose ls 获取的项目
func manageContainers(view *ui.UI, rt runtime.Runtime) error {
	conts, err := listContainers(rt)
	if err != nil {
		return err
	}
	if len(conts) == 0 {
		log.Info("未发现容器")
		view.Pause()
		return ui.ErrReturnToMenu
	}

	for {
		ui.ClearScreen()
		ui.MenuTitle("容器管理", "容器列表与操作")
		printContainers(conts)
		fmt.Println()

		raw, err := view.Ask("请选择容器编号（0/q 返回）：")
		if err != nil {
			return err
		}
		fmt.Println()

		if ui.IsReturnChoice(raw) {
			return ui.ErrReturnToMenu
		}

		idx, err := input.Number(raw, 1, len(conts))
		if err != nil || idx < 1 || idx > len(conts) {
			ui.InvalidChoice()
			view.Pause()
			continue
		}

		c := conts[idx-1]
		if err := manageSingleContainer(view, rt, c); err != nil {
			return err
		}

		// refresh
		conts, err = listContainers(rt)
		if err != nil {
			return err
		}
		if len(conts) == 0 {
			return ui.ErrReturnToMenu
		}
	}
}

func printContainers(conts []containerInfo) {
	fmt.Println(ui.PrimaryBoldText("容器列表："))
	fmt.Println(ui.InfoText(fmt.Sprintf("%-4s %-8s %-12s %-24s %-34s %-18s %s", "编号", "状态", "ID", "名称", "端口", "运行/退出时间", "创建时间")))
	for i, c := range conts {
		state := colorContainerState(c.State, containerStateDisplay(c.State))
		fmt.Printf(
			"%-4s %s %-12s %-24s %-34s %-18s %s\n",
			fmt.Sprintf("%d)", i+1),
			ui.TableCell(state, 8),
			shortContainerID(c.ID),
			listing.TruncateText(listing.DefaultText(c.Name), 24),
			listing.TruncateText(listing.DefaultText(c.Ports), 34),
			listing.TruncateText(listing.DefaultText(containerTimeText(c)), 18),
			listing.DefaultText(c.CreatedAt),
		)
	}
}

func manageSingleContainer(view *ui.UI, rt runtime.Runtime, c containerInfo) error {
	compose, composeErr := update.DetectComposeCommandForRuntime(rt.Name)
	project := getContainerComposeProject(rt, c)
	canComposeDown := project != "" && composeErr == nil

	for {
		ui.ClearScreen()
		ui.MenuTitle("容器管理", "容器列表与操作", listing.DefaultText(c.Name))
		ui.PrintInfoCard("容器关键信息",
			ui.CardField{Label: "容器", Value: fmt.Sprintf("%s（%s）", c.Name, c.ID)},
			ui.CardField{Label: "状态", Value: containerDetailStatus(c)},
			ui.CardField{Label: "端口", Value: listing.DefaultText(c.Ports)},
		)
		fmt.Println()

		actions := availableContainerActions(c, canComposeDown)
		for index, action := range actions {
			command := rt.Name + " " + action.Command
			if action.Key == "down" {
				command = compose.Display + " " + action.Command
			}
			ui.MenuOptionHint(strconv.Itoa(index+1), action.Label, command)
		}
		ui.MenuExit("0/q", "返回")

		raw, err := view.Ask("请选择：")
		if err != nil {
			return err
		}
		fmt.Println()

		if ui.IsReturnChoice(raw) {
			return nil
		}
		index, err := input.Number(raw, 1, len(actions))
		if err != nil || index < 1 || index > len(actions) {
			ui.InvalidChoice()
			view.PauseWithPrompt("按回车返回容器操作...")
			continue
		}

		switch actions[index-1].Key {
		case "start":
			runContainerLifecycle(rt, c, "start", "启动失败: ", "已启动: ")
		case "stop":
			runContainerLifecycle(rt, c, "stop", "停止失败: ", "已停止: ")
		case "restart":
			runContainerLifecycle(rt, c, "restart", "重启失败: ", "已重启: ")
		case "pause":
			runContainerLifecycle(rt, c, "pause", "暂停失败: ", "已暂停: ")
		case "unpause":
			runContainerLifecycle(rt, c, "unpause", "恢复失败: ", "已恢复: ")
		case "inspect":
			runContainerInspect(rt, c)
			view.PauseWithPrompt("按回车返回容器操作...")
			continue
		case "logs":
			runContainerLogs(rt, c, false)
			view.PauseWithPrompt("按回车返回容器操作...")
			continue
		case "logs-follow":
			if !runContainerLogs(rt, c, true) {
				view.PauseWithPrompt("按回车返回容器操作...")
			}
			continue
		case "exec":
			if !runContainerShell(rt, c) {
				view.PauseWithPrompt("按回车返回容器操作...")
			}
			continue
		case "down":
			dir, _ := composeprojects.FindProjectDir(compose, project)
			if dir == "" {
				log.Warn("未能找到项目目录，跳过 down")
				view.PauseWithPrompt("按回车返回容器操作...")
				continue
			}
			confirmed, err := view.Confirm(fmt.Sprintf("项目目录：%s\n确认对项目 %s 执行 down（不会删除卷）？(y/N)：", dir, project))
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
			if err := update.RunCompose(compose, dir, "down"); err != nil {
				log.Error("down 失败: ", err)
			} else {
				log.Info("已 down 项目: ", project)
			}
		case "rm":
			executed, err := removeContainer(
				view.Confirm,
				system.Run,
				rt.Name,
				c,
			)
			if err != nil {
				if errors.Is(err, ui.ErrReturnToMenu) {
					continue
				}
				log.Error("删除容器失败: ", err)
			}
			if !executed {
				if err != nil {
					view.PauseWithPrompt("按回车返回容器操作...")
				}
				continue
			}
		}
		view.Pause()
		return nil // return after action to refresh outer list
	}
}

func availableContainerActions(c containerInfo, canComposeDown bool) []containerAction {
	actions := make([]containerAction, 0, 11)
	if canStartContainer(c) {
		actions = append(actions, containerAction{Key: "start", Label: "启动容器", Command: "start"})
	}
	if canStopContainer(c) {
		actions = append(actions, containerAction{Key: "stop", Label: "停止容器", Command: "stop"})
	}
	actions = append(actions, containerAction{Key: "restart", Label: "重启容器", Command: "restart"})
	if c.State == listing.StateRunning {
		actions = append(actions, containerAction{Key: "pause", Label: "暂停容器", Command: "pause"})
	} else if c.State == listing.StatePaused {
		actions = append(actions, containerAction{Key: "unpause", Label: "恢复容器", Command: "unpause"})
	}
	actions = append(actions,
		containerAction{Key: "inspect", Label: "查看容器信息", Command: "inspect"},
		containerAction{Key: "logs", Label: "查看最近 200 行日志", Command: "logs --tail 200"},
		containerAction{Key: "logs-follow", Label: "实时跟随日志", Command: "logs -f"},
	)
	if canEnterContainer(c) {
		actions = append(actions, containerAction{Key: "exec", Label: "进入容器 Shell", Command: "exec"})
	}
	if canComposeDown {
		actions = append(actions, containerAction{Key: "down", Label: "删除所属 Compose 项目", Command: "down"})
	}
	if canRemoveContainer(c) {
		actions = append(actions, containerAction{Key: "rm", Label: "删除已停止容器", Command: "rm"})
	}
	return actions
}

func canEnterContainer(c containerInfo) bool {
	return c.State == listing.StateRunning
}

func canStartContainer(c containerInfo) bool {
	switch c.State {
	case listing.StateRunning, listing.StatePaused, listing.StateRestarting:
		return false
	default:
		return true
	}
}

func canStopContainer(c containerInfo) bool {
	switch c.State {
	case listing.StateRunning, listing.StatePaused, listing.StateRestarting:
		return true
	default:
		return false
	}
}

func canRemoveContainer(c containerInfo) bool {
	switch c.State {
	case listing.StateExited, listing.StateCreated, listing.StateDead:
		return true
	default:
		return false
	}
}

func removeContainer(confirm func(string) (bool, error), run func(string, ...string) error, runtimeName string, c containerInfo) (bool, error) {
	if !canRemoveContainer(c) {
		return false, fmt.Errorf("当前状态不允许删除容器")
	}
	if c.ID == "" {
		return false, fmt.Errorf("未取得容器 ID，无法安全删除，请刷新后重试")
	}
	prompt := fmt.Sprintf("容器：%s\nID：%s\n确认执行 rm（不会强制删除）？(y/N)：", listing.DefaultText(c.Name), listing.DefaultText(c.ID))
	confirmed, err := confirm(prompt)
	if err != nil || !confirmed {
		return false, err
	}
	if err := run(runtimeName, containerRemoveArgs(c.ID)...); err != nil {
		return true, err
	}
	log.Info("已删除容器: ", containerRef(c))
	return true, nil
}

func runContainerLifecycle(rt runtime.Runtime, c containerInfo, action, failureMessage, successMessage string) {
	ref := containerRef(c)
	if err := system.Run(rt.Name, containerLifecycleArgs(action, ref)...); err != nil {
		log.Error(failureMessage, err)
		return
	}
	log.Info(successMessage, ref)
}

func runContainerLogs(rt runtime.Runtime, c containerInfo, follow bool) bool {
	if err := system.Run(rt.Name, containerLogsArgs(containerRef(c), follow)...); err != nil {
		if follow && system.IsInterrupted(err) {
			log.Info("已退出实时日志")
			return true
		}
		log.Error("查看日志失败: ", err)
		return false
	}
	return true
}

func runContainerInspect(rt runtime.Runtime, c containerInfo) bool {
	ref := containerRef(c)
	if err := system.Run(rt.Name, containerInspectArgs(ref)...); err != nil {
		log.Error("查看容器信息失败: ", err)
		return false
	}
	return true
}

func runContainerShell(rt runtime.Runtime, c containerInfo) bool {
	fmt.Println("进入容器 Shell，输入 exit 或 Ctrl+D 返回容器操作界面。")
	if err := system.Run(rt.Name, containerShellArgs(containerRef(c))...); err != nil {
		if system.IsInterrupted(err) {
			log.Info("已退出容器 Shell")
			return true
		}
		log.Error("进入容器失败: ", err)
		return false
	}
	log.Info("已退出容器 Shell")
	return true
}

func containerLifecycleArgs(action, ref string) []string {
	return []string{action, ref}
}

func containerShellArgs(ref string) []string {
	return []string{
		"exec",
		"-it",
		ref,
		"sh",
		"-lc",
		"if command -v bash >/dev/null 2>&1; then bash; else sh; fi; exit 0",
	}
}

func containerLogsArgs(ref string, follow bool) []string {
	if follow {
		return []string{"logs", "-f", "--tail", "100", ref}
	}
	return []string{"logs", "--tail", "200", ref}
}

func containerInspectArgs(ref string) []string {
	return []string{"inspect", ref}
}

func containerRemoveArgs(ref string) []string {
	return []string{"rm", ref}
}

func containerRef(c containerInfo) string {
	if c.Name != "" {
		return c.Name
	}
	return c.ID
}

func containerStateDisplay(state string) string {
	switch state {
	case listing.StateRunning:
		return "运行中"
	case listing.StateExited:
		return "已停止"
	case listing.StatePaused:
		return "已暂停"
	case listing.StateCreated:
		return "已创建"
	case listing.StateRestarting:
		return "重启中"
	case "dead":
		return "异常"
	case "removing":
		return "删除中"
	default:
		return "未知"
	}
}

func containerDetailStatus(c containerInfo) string {
	display := containerStateDisplay(c.State)
	if c.Status == "" {
		return colorContainerState(c.State, display)
	}
	return colorContainerState(c.State, fmt.Sprintf("%s（%s）", display, c.Status))
}

func colorContainerState(state, text string) string {
	switch state {
	case listing.StateRunning:
		return ui.SuccessText(text)
	case listing.StateExited, listing.StatePaused, listing.StateCreated:
		return ui.WarningText(text)
	case listing.StateRestarting:
		return ui.InfoText(text)
	case listing.StateDead, "removing":
		return ui.DangerText(text)
	default:
		return ui.MutedText(text)
	}
}

func shortContainerID(id string) string {
	runes := []rune(strings.TrimSpace(id))
	if len(runes) <= 12 {
		return string(runes)
	}
	return string(runes[:12])
}

func containerTimeText(c containerInfo) string {
	if c.RunningFor != "" {
		return c.RunningFor
	}
	return c.Status
}

func getContainerComposeProject(rt runtime.Runtime, c containerInfo) string {
	ref := containerRef(c)
	label, _ := system.Output(rt.Name, "inspect", "-f", `{{ index .Config.Labels "com.docker.compose.project" }}`, ref)
	if strings.TrimSpace(label) == "" {
		label, _ = system.Output(rt.Name, "inspect", "-f", `{{ index .Config.Labels "io.podman.compose.project" }}`, ref)
	}
	return strings.TrimSpace(label)
}
