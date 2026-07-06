package list

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"snail_tool/internal/container/runtime"
	"snail_tool/internal/container/update"
	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

type containerInfo struct {
	ID         string
	Name       string
	Ports      string
	Status     string
	CreatedAt  string
	RunningFor string
	IsRunning  bool
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
func ManageComposeLS(view *ui.UI) error {
	rt, ok := runtime.Detect()
	if !ok {
		return fmt.Errorf("未检测到 Docker 或 Podman")
	}
	return manageComposeProjects(view, rt, true)
}

// ManageComposeScan 入口：管理通过扫描目录获取的 docker compose 项目
func ManageComposeScan(view *ui.UI) error {
	rt, ok := runtime.Detect()
	if !ok {
		return fmt.Errorf("未检测到 Docker 或 Podman")
	}
	return manageComposeProjects(view, rt, false)
}

func manageContainers(view *ui.UI, rt runtime.Runtime) error {
	conts, err := listContainers(rt)
	if err != nil {
		return err
	}
	if len(conts) == 0 {
		log.Info("未发现容器")
		view.Pause()
		return shared.ErrReturnToMenu
	}

	for {
		ui.ClearScreen()
		printContainers(conts)
		fmt.Println()

		raw, err := view.Ask("选择容器编号（0 返回）：")
		if err != nil {
			return err
		}
		fmt.Println()

		if shared.IsReturnChoice(raw) || strings.TrimSpace(raw) == "0" {
			return shared.ErrReturnToMenu
		}

		idx, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || idx < 1 || idx > len(conts) {
			fmt.Println("无效编号，请重试")
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
			return shared.ErrReturnToMenu
		}
	}
}

func listContainers(rt runtime.Runtime) ([]containerInfo, error) {
	format := "{{.ID}}\t{{.Names}}\t{{.Ports}}\t{{.Status}}\t{{.CreatedAt}}\t{{.RunningFor}}"
	out, err := system.Output(rt.Name, "ps", "-a", "--format", format)
	if err != nil {
		return nil, fmt.Errorf("获取容器列表失败: %w", err)
	}

	var conts []containerInfo
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 6 {
			continue
		}
		c := containerInfo{
			ID:         parts[0],
			Name:       parts[1],
			Ports:      parts[2],
			Status:     parts[3],
			CreatedAt:  parts[4],
			RunningFor: parts[5],
		}
		lower := strings.ToLower(c.Status)
		c.IsRunning = strings.HasPrefix(lower, "up") || strings.Contains(lower, "running")
		conts = append(conts, c)
	}
	return conts, nil
}

func printContainers(conts []containerInfo) {
	fmt.Println("容器列表（ID 名称 端口 运行时间 启动时间）：")
	for i, c := range conts {
		id := c.ID
		if len(id) > 12 {
			id = id[:12]
		}
		fmt.Printf("%d) %s  %s  %s  %s  %s\n", i+1, id, c.Name, c.Ports, c.RunningFor, c.CreatedAt)
	}
}

func manageSingleContainer(view *ui.UI, rt runtime.Runtime, c containerInfo) error {
	compose, _ := update.DetectComposeCommand()
	project := getContainerComposeProject(rt, c)

	for {
		fmt.Printf("\n容器: %s (%s)\n", c.Name, c.ID)
		fmt.Printf("状态: %s | 端口: %s\n", c.Status, c.Ports)
		fmt.Println()

		fmt.Println("请选择操作：")
		if c.IsRunning {
			fmt.Println("1) 停止")
			fmt.Println("2) 重启")
		} else {
			fmt.Println("1) 启动")
		}
		if project != "" {
			fmt.Println("3) Down（compose down）")
		}
		fmt.Println("0/q) 返回")
		fmt.Println()

		raw, err := view.Ask("输入选项: ")
		if err != nil {
			return err
		}
		fmt.Println()

		ch := strings.ToLower(strings.TrimSpace(raw))
		if shared.IsReturnChoice(ch) || ch == "0" {
			return nil
		}

		switch ch {
		case "1":
			if c.IsRunning {
				if err := system.Run(rt.Name, "stop", c.Name); err != nil {
					log.Error("停止失败: ", err)
				} else {
					log.Info("已停止: ", c.Name)
					c.IsRunning = false
				}
			} else {
				if err := system.Run(rt.Name, "start", c.Name); err != nil {
					log.Error("启动失败: ", err)
				} else {
					log.Info("已启动: ", c.Name)
					c.IsRunning = true
				}
			}
		case "2":
			if err := system.Run(rt.Name, "restart", c.Name); err != nil {
				log.Error("重启失败: ", err)
			} else {
				log.Info("已重启: ", c.Name)
			}
		case "3":
			if project != "" {
				dir, _ := findProjectDirForContainer(compose, project)
				if dir != "" {
					confirmed, _ := view.Confirm(fmt.Sprintf("确认对项目 %s 执行 down？(y/N): ", project))
					if confirmed {
						if err := update.RunCompose(compose, dir, "down"); err != nil {
							log.Error("Down 失败: ", err)
						} else {
							log.Info("已 down 项目: ", project)
						}
					}
				} else {
					log.Warn("未能找到项目目录，跳过 down")
				}
			}
		default:
			fmt.Println("无效选项")
		}
		view.Pause()
		return nil // return after action to refresh outer list
	}
}

func getContainerComposeProject(rt runtime.Runtime, c containerInfo) string {
	label, _ := system.Output(rt.Name, "inspect", "-f", `{{ index .Config.Labels "com.docker.compose.project" }}`, c.Name)
	if strings.TrimSpace(label) == "" {
		label, _ = system.Output(rt.Name, "inspect", "-f", `{{ index .Config.Labels "io.podman.compose.project" }}`, c.Name)
	}
	return strings.TrimSpace(label)
}

func findProjectDirForContainer(compose update.ComposeCommand, project string) (string, error) {
	if project == "" {
		return "", nil
	}
	projs, err := update.GetAllComposeProjectDirsFromLS(compose)
	if err != nil || len(projs) == 0 {
		// fallback: try to use ls json for name->dir map
	}
	// re-fetch map
	output, err := system.Output(compose.Name, append(compose.Args, "ls", "--format", "json")...)
	if err != nil {
		return "", err
	}
	var projects []struct {
		Name        string `json:"Name"`
		ConfigFiles string `json:"ConfigFiles"`
	}
	if err := json.Unmarshal([]byte(output), &projects); err != nil {
		return "", err
	}
	for _, p := range projects {
		if p.Name == project && p.ConfigFiles != "" {
			for _, f := range strings.Split(p.ConfigFiles, ",") {
				f = strings.TrimSpace(f)
				if f != "" {
					return filepath.Dir(f), nil
				}
			}
		}
	}
	return "", nil
}

func manageComposeProjects(view *ui.UI, rt runtime.Runtime, useLS bool) error {
	compose, err := update.DetectComposeCommand()
	if err != nil {
		return err
	}

	var dirs []string
	if useLS {
		dirs, err = update.GetAllComposeProjectDirsFromLS(compose)
		if err != nil {
			return err
		}
	} else {
		_, dirs, err = update.AskComposeScanDirs(view, update.DefaultComposeRoots())
		if err != nil {
			return err
		}
	}

	if len(dirs) == 0 {
		log.Info("未发现 Compose 项目")
		view.Pause()
		return shared.ErrReturnToMenu
	}

	for {
		ui.ClearScreen()
		fmt.Println("Compose 项目列表：")
		for i, d := range dirs {
			fmt.Printf("%d) %s\n", i+1, d)
		}
		fmt.Println()

		raw, err := view.Ask("选择项目编号（0 返回）：")
		if err != nil {
			return err
		}
		fmt.Println()

		if shared.IsReturnChoice(raw) || strings.TrimSpace(raw) == "0" {
			return shared.ErrReturnToMenu
		}

		idx, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || idx < 1 || idx > len(dirs) {
			fmt.Println("无效编号")
			view.Pause()
			continue
		}

		dir := dirs[idx-1]
		if err := manageSingleProject(view, compose, dir); err != nil {
			return err
		}
		// no auto refresh for projects list, return
		return shared.ErrReturnToMenu
	}
}

func manageSingleProject(view *ui.UI, compose update.ComposeCommand, dir string) error {
	fmt.Printf("\n项目目录: %s\n", dir)
	fmt.Println("请选择操作：")
	fmt.Println("1) Down")
	fmt.Println("2) Stop")
	fmt.Println("3) Restart")
	fmt.Println("0/q) 返回")
	fmt.Println()

	raw, err := view.Ask("输入选项: ")
	if err != nil {
		return err
	}
	fmt.Println()

	ch := strings.ToLower(strings.TrimSpace(raw))
	if shared.IsReturnChoice(ch) || ch == "0" {
		return shared.ErrReturnToMenu
	}

	var action string
	switch ch {
	case "1":
		action = "down"
	case "2":
		action = "stop"
	case "3":
		action = "restart"
	default:
		fmt.Println("无效选项")
		view.Pause()
		return shared.ErrReturnToMenu
	}

	confirmed, _ := view.Confirm(fmt.Sprintf("确认执行 compose %s 于 %s ？(y/N): ", action, dir))
	if !confirmed {
		fmt.Println("已取消")
		view.Pause()
		return shared.ErrReturnToMenu
	}

	if err := update.RunCompose(compose, dir, action); err != nil {
		log.Error("执行失败: ", err)
	} else {
		log.Info("已执行 compose ", action, " 于 ", dir)
	}
	view.Pause()
	return shared.ErrReturnToMenu
}
