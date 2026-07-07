package list

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
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
	State      string
	CreatedAt  string
	RunningFor string
	IsRunning  bool
}

type composeProjectInfo struct {
	Name        string
	Dir         string
	Status      string
	StatusError string
	Containers  composeContainerSummary
}

type composeContainerSummary struct {
	HasData    bool
	Total      int
	Running    int
	Exited     int
	Paused     int
	Restarting int
	Created    int
	Unknown    int
}

type composeProjectAction struct {
	Name string
	Args []string
}

const (
	containerStateRunning    = "running"
	containerStateExited     = "exited"
	containerStatePaused     = "paused"
	containerStateCreated    = "created"
	containerStateRestarting = "restarting"
	containerStateUnknown    = "unknown"
)

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
	out, err := system.Output(rt.Name, "ps", "-a", "--format", "json")
	if err == nil {
		conts, parseErr := parseContainersJSON(out)
		if parseErr == nil {
			return conts, nil
		}
	}

	conts, fallbackErr := listContainersText(rt)
	if fallbackErr != nil {
		if err != nil {
			return nil, fmt.Errorf("获取容器列表失败: %w", err)
		}
		return nil, fmt.Errorf("获取容器列表失败: %w", fallbackErr)
	}
	return conts, nil
}

func listContainersText(rt runtime.Runtime) ([]containerInfo, error) {
	format := "{{.ID}}\t{{.Names}}\t{{.Ports}}\t{{.Status}}\t{{.CreatedAt}}\t{{.RunningFor}}"
	out, err := system.Output(rt.Name, "ps", "-a", "--format", format)
	if err != nil {
		return nil, err
	}
	return parseContainersText(out), nil
}

func parseContainersText(out string) []containerInfo {
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
		if len(parts) >= 7 {
			c.State = parts[6]
		}
		conts = append(conts, normalizeContainer(c))
	}
	return conts
}

func parseContainersJSON(out string) ([]containerInfo, error) {
	raw := strings.TrimSpace(out)
	if raw == "" {
		return nil, nil
	}

	if strings.HasPrefix(raw, "[") {
		var rows []map[string]any
		if err := json.Unmarshal([]byte(raw), &rows); err != nil {
			return nil, err
		}
		return containersFromJSONObjects(rows), nil
	}

	var rows []map[string]any
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return containersFromJSONObjects(rows), nil
}

func containersFromJSONObjects(rows []map[string]any) []containerInfo {
	conts := make([]containerInfo, 0, len(rows))
	for _, row := range rows {
		c := containerInfo{
			ID:         jsonStringField(row, "ID", "Id", "id", "ContainerID"),
			Name:       jsonStringField(row, "Names", "Name", "names", "name"),
			Ports:      jsonStringField(row, "Ports", "ports"),
			Status:     jsonStringField(row, "Status", "status"),
			State:      jsonStringField(row, "State", "state"),
			CreatedAt:  jsonStringField(row, "CreatedAt", "Created", "createdAt", "created"),
			RunningFor: jsonStringField(row, "RunningFor", "runningFor"),
		}
		conts = append(conts, normalizeContainer(c))
	}
	return conts
}

func jsonStringField(row map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := row[key]; ok {
			return strings.TrimSpace(jsonValueString(value))
		}
	}
	return ""
}

func jsonValueString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			text := strings.TrimSpace(jsonValueString(item))
			if text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		if text := formatPortMap(v); text != "" {
			return text
		}
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(raw)
	default:
		return fmt.Sprint(v)
	}
}

func formatPortMap(port map[string]any) string {
	containerPort := jsonStringField(port, "container_port", "ContainerPort", "containerPort")
	hostPort := jsonStringField(port, "host_port", "HostPort", "hostPort")
	hostIP := jsonStringField(port, "host_ip", "HostIP", "hostIp")
	protocol := jsonStringField(port, "protocol", "Protocol")

	if containerPort == "" && hostPort == "" {
		return ""
	}
	if protocol == "" {
		protocol = "tcp"
	}
	target := containerPort
	if target != "" {
		target += "/" + protocol
	}
	if hostPort == "" {
		return target
	}
	if hostIP != "" {
		return fmt.Sprintf("%s:%s->%s", hostIP, hostPort, target)
	}
	return fmt.Sprintf("%s->%s", hostPort, target)
}

func normalizeContainer(c containerInfo) containerInfo {
	c.ID = strings.TrimSpace(c.ID)
	c.Name = strings.TrimSpace(c.Name)
	c.Ports = strings.TrimSpace(c.Ports)
	c.Status = strings.TrimSpace(c.Status)
	c.State = normalizeContainerState(c.State, c.Status)
	c.CreatedAt = strings.TrimSpace(c.CreatedAt)
	c.RunningFor = strings.TrimSpace(c.RunningFor)
	c.IsRunning = c.State == containerStateRunning
	return c
}

func normalizeContainerState(state, status string) string {
	lowerState := strings.ToLower(strings.TrimSpace(state))
	switch lowerState {
	case containerStateRunning, containerStateExited, containerStatePaused, containerStateCreated, containerStateRestarting:
		return lowerState
	case "dead", "removing":
		return lowerState
	}

	lowerStatus := strings.ToLower(strings.TrimSpace(status))
	switch {
	case strings.Contains(lowerStatus, "paused"):
		return containerStatePaused
	case strings.Contains(lowerStatus, "restarting"):
		return containerStateRestarting
	case strings.HasPrefix(lowerStatus, "up") || strings.Contains(lowerStatus, "running"):
		return containerStateRunning
	case strings.HasPrefix(lowerStatus, "exited") || strings.HasPrefix(lowerStatus, "dead"):
		return containerStateExited
	case strings.HasPrefix(lowerStatus, "created"):
		return containerStateCreated
	default:
		return containerStateUnknown
	}
}

func printContainers(conts []containerInfo) {
	fmt.Println("容器列表：")
	fmt.Printf("%-4s %-8s %-12s %-24s %-34s %-18s %s\n", "编号", "状态", "ID", "名称", "端口", "运行/退出时间", "创建时间")
	for i, c := range conts {
		fmt.Printf(
			"%-4s %-8s %-12s %-24s %-34s %-18s %s\n",
			fmt.Sprintf("%d)", i+1),
			containerStateDisplay(c.State),
			shortContainerID(c.ID),
			truncateText(defaultText(c.Name), 24),
			truncateText(defaultText(c.Ports), 34),
			truncateText(defaultText(containerTimeText(c)), 18),
			defaultText(c.CreatedAt),
		)
	}
}

func manageSingleContainer(view *ui.UI, rt runtime.Runtime, c containerInfo) error {
	compose, composeErr := update.DetectComposeCommand()
	project := getContainerComposeProject(rt, c)
	canComposeDown := project != "" && composeErr == nil

	for {
		ui.ClearScreen()
		fmt.Printf("\n容器: %s (%s)\n", c.Name, c.ID)
		fmt.Printf("状态: %s | 端口: %s\n", containerDetailStatus(c), defaultText(c.Ports))
		fmt.Println()

		fmt.Println("请选择操作：")
		if canStartContainer(c) {
			fmt.Println("1) 启动")
		}
		if canStopContainer(c) {
			fmt.Println("2) 停止")
		}
		fmt.Println("3) 重启")
		fmt.Println("4) 查看容器信息（inspect）")
		fmt.Println("5) 查看日志（最近 200 行）")
		fmt.Println("6) 实时日志（Ctrl+C 返回操作界面）")
		if canEnterContainer(c) {
			fmt.Println("7) 进入容器 Shell（exit/Ctrl+D 返回操作界面）")
		}
		if canComposeDown {
			fmt.Println("8) Down（compose down）")
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
			if !canStartContainer(c) {
				fmt.Println("当前状态不支持启动")
				view.PauseWithPrompt("按回车返回容器操作...")
				continue
			}
			runContainerLifecycle(rt, c, "start", "启动失败: ", "已启动: ")
		case "2":
			if !canStopContainer(c) {
				fmt.Println("当前状态不支持停止")
				view.PauseWithPrompt("按回车返回容器操作...")
				continue
			}
			runContainerLifecycle(rt, c, "stop", "停止失败: ", "已停止: ")
		case "3":
			runContainerLifecycle(rt, c, "restart", "重启失败: ", "已重启: ")
		case "4":
			runContainerInspect(rt, c)
			view.PauseWithPrompt("按回车返回容器操作...")
			continue
		case "5":
			runContainerLogs(rt, c, false)
			view.PauseWithPrompt("按回车返回容器操作...")
			continue
		case "6":
			if !runContainerLogs(rt, c, true) {
				view.PauseWithPrompt("按回车返回容器操作...")
			}
			continue
		case "7":
			if !canEnterContainer(c) {
				fmt.Println("当前状态不支持进入容器")
				view.PauseWithPrompt("按回车返回容器操作...")
				continue
			}
			if !runContainerShell(rt, c) {
				view.PauseWithPrompt("按回车返回容器操作...")
			}
			continue
		case "8":
			if canComposeDown {
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
			} else if project != "" {
				log.Warn("未检测到 Compose 命令，无法执行 down")
			} else {
				fmt.Println("当前容器未检测到 Compose 项目")
				view.PauseWithPrompt("按回车返回容器操作...")
				continue
			}
		default:
			fmt.Println("无效选项")
			view.PauseWithPrompt("按回车返回容器操作...")
			continue
		}
		view.Pause()
		return nil // return after action to refresh outer list
	}
}

func canEnterContainer(c containerInfo) bool {
	return c.State == containerStateRunning
}

func canStartContainer(c containerInfo) bool {
	switch c.State {
	case containerStateRunning, containerStatePaused, containerStateRestarting:
		return false
	default:
		return true
	}
}

func canStopContainer(c containerInfo) bool {
	switch c.State {
	case containerStateRunning, containerStatePaused, containerStateRestarting:
		return true
	default:
		return false
	}
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

func containerRef(c containerInfo) string {
	if c.Name != "" {
		return c.Name
	}
	return c.ID
}

func containerStateDisplay(state string) string {
	switch state {
	case containerStateRunning:
		return "运行中"
	case containerStateExited:
		return "已停止"
	case containerStatePaused:
		return "已暂停"
	case containerStateCreated:
		return "已创建"
	case containerStateRestarting:
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
		return display
	}
	return fmt.Sprintf("%s（%s）", display, c.Status)
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

func defaultText(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func truncateText(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}

func getContainerComposeProject(rt runtime.Runtime, c containerInfo) string {
	ref := containerRef(c)
	label, _ := system.Output(rt.Name, "inspect", "-f", `{{ index .Config.Labels "com.docker.compose.project" }}`, ref)
	if strings.TrimSpace(label) == "" {
		label, _ = system.Output(rt.Name, "inspect", "-f", `{{ index .Config.Labels "io.podman.compose.project" }}`, ref)
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

func listComposeProjectsFromLS(compose update.ComposeCommand) ([]composeProjectInfo, error) {
	output, err := composeOutputGlobal(compose, "ls", "--format", "json")
	if err != nil {
		return nil, fmt.Errorf("执行 compose ls 失败: %w", err)
	}

	output = strings.TrimSpace(output)
	if output == "" || output == "[]" || output == "null" {
		return nil, nil
	}

	var rows []struct {
		Name        string `json:"Name"`
		Status      string `json:"Status"`
		ConfigFiles string `json:"ConfigFiles"`
	}
	if err := json.Unmarshal([]byte(output), &rows); err != nil {
		return nil, fmt.Errorf("解析 compose ls 输出失败: %w (原始: %s)", err, output)
	}

	byDir := make(map[string]composeProjectInfo)
	for _, row := range rows {
		for _, file := range strings.Split(row.ConfigFiles, ",") {
			file = strings.TrimSpace(file)
			if file == "" {
				continue
			}
			dir := filepath.Clean(filepath.Dir(file))
			if dir == "." || dir == "" {
				continue
			}
			byDir[dir] = composeProjectInfo{
				Name:   strings.TrimSpace(row.Name),
				Dir:    dir,
				Status: strings.TrimSpace(row.Status),
			}
		}
	}

	return enrichComposeProjects(compose, sortComposeProjects(mapComposeProjects(byDir))), nil
}

func listComposeProjectsFromDirs(compose update.ComposeCommand, dirs []string) []composeProjectInfo {
	projects := make([]composeProjectInfo, 0, len(dirs))
	for _, dir := range dirs {
		dir = filepath.Clean(strings.TrimSpace(dir))
		if dir == "" {
			continue
		}
		projects = append(projects, composeProjectInfo{
			Name: filepath.Base(dir),
			Dir:  dir,
		})
	}
	return enrichComposeProjects(compose, sortComposeProjects(projects))
}

func mapComposeProjects(projects map[string]composeProjectInfo) []composeProjectInfo {
	result := make([]composeProjectInfo, 0, len(projects))
	for _, project := range projects {
		result = append(result, project)
	}
	return result
}

func sortComposeProjects(projects []composeProjectInfo) []composeProjectInfo {
	sort.Slice(projects, func(i, j int) bool {
		if projects[i].Name != projects[j].Name {
			return projects[i].Name < projects[j].Name
		}
		return projects[i].Dir < projects[j].Dir
	})
	return projects
}

func enrichComposeProjects(compose update.ComposeCommand, projects []composeProjectInfo) []composeProjectInfo {
	for i := range projects {
		summary, projectName, err := composeProjectContainerSummary(compose, projects[i].Dir)
		if err != nil {
			projects[i].StatusError = err.Error()
			continue
		}
		projects[i].Containers = summary
		if projects[i].Name == "" && projectName != "" {
			projects[i].Name = projectName
		}
	}
	return projects
}

func composeProjectContainerSummary(compose update.ComposeCommand, dir string) (composeContainerSummary, string, error) {
	output, err := composeOutputInDir(compose, dir, "ps", "--format", "json")
	if err == nil {
		summary, projectName, parseErr := parseComposePSJSON(output)
		if parseErr == nil {
			return summary, projectName, nil
		}
	}

	output, fallbackErr := composeOutputInDir(compose, dir, "ps")
	if fallbackErr != nil {
		if err != nil {
			return composeContainerSummary{}, "", fmt.Errorf("compose ps 失败: %w", err)
		}
		return composeContainerSummary{}, "", fmt.Errorf("compose ps 失败: %w", fallbackErr)
	}
	return parseComposePSText(output), "", nil
}

func parseComposePSJSON(output string) (composeContainerSummary, string, error) {
	raw := strings.TrimSpace(output)
	if raw == "" || raw == "[]" || raw == "null" {
		return composeContainerSummary{HasData: true}, "", nil
	}

	var rows []map[string]any
	if strings.HasPrefix(raw, "[") {
		if err := json.Unmarshal([]byte(raw), &rows); err != nil {
			return composeContainerSummary{}, "", err
		}
	} else {
		for _, line := range strings.Split(raw, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var row map[string]any
			if err := json.Unmarshal([]byte(line), &row); err != nil {
				return composeContainerSummary{}, "", err
			}
			rows = append(rows, row)
		}
	}

	summary := composeContainerSummary{HasData: true}
	projectName := ""
	for _, row := range rows {
		state := normalizeContainerState(
			jsonStringField(row, "State", "state"),
			jsonStringField(row, "Status", "status"),
		)
		summary.addState(state)
		if projectName == "" {
			projectName = jsonStringField(row, "Project", "project")
		}
	}
	return summary, projectName, nil
}

func parseComposePSText(output string) composeContainerSummary {
	summary := composeContainerSummary{HasData: true}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(strings.ToLower(line), "name ") || strings.Contains(strings.ToLower(line), "no containers") {
			continue
		}
		summary.addState(normalizeComposePSTextState(line))
	}
	return summary
}

func normalizeComposePSTextState(line string) string {
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "paused"):
		return containerStatePaused
	case strings.Contains(lower, "restarting"):
		return containerStateRestarting
	case strings.Contains(lower, "exited"):
		return containerStateExited
	case strings.Contains(lower, "created"):
		return containerStateCreated
	case strings.Contains(lower, "dead"):
		return "dead"
	case strings.Contains(lower, "running") || strings.Contains(lower, " up "):
		return containerStateRunning
	default:
		return containerStateUnknown
	}
}

func (s *composeContainerSummary) addState(state string) {
	s.HasData = true
	s.Total++
	switch state {
	case containerStateRunning:
		s.Running++
	case containerStateExited, "dead":
		s.Exited++
	case containerStatePaused:
		s.Paused++
	case containerStateRestarting:
		s.Restarting++
	case containerStateCreated:
		s.Created++
	default:
		s.Unknown++
	}
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
	fmt.Println("Compose 项目列表：")
	fmt.Printf("%-4s %-16s %-28s %-24s %s\n", "编号", "项目状态", "容器状态", "项目名", "目录")
	for i, project := range projects {
		fmt.Printf(
			"%-4s %-16s %-28s %-24s %s\n",
			fmt.Sprintf("%d)", i+1),
			truncateText(defaultText(composeProjectStatusDisplay(project)), 16),
			truncateText(defaultText(composeContainerSummaryDisplay(project.Containers)), 28),
			truncateText(defaultText(project.Name), 24),
			defaultText(project.Dir),
		)
	}
}

func composeOutputGlobal(compose update.ComposeCommand, args ...string) (string, error) {
	out, err := exec.Command(compose.Name, composeArgs(compose, args...)...).CombinedOutput()
	return string(out), err
}

func composeOutputInDir(compose update.ComposeCommand, dir string, args ...string) (string, error) {
	cmd := exec.Command(compose.Name, composeArgs(compose, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func composeArgs(compose update.ComposeCommand, args ...string) []string {
	allArgs := append([]string{}, compose.Args...)
	return append(allArgs, args...)
}

func manageComposeProjects(view *ui.UI, rt runtime.Runtime, useLS bool) error {
	compose, err := update.DetectComposeCommand()
	if err != nil {
		return err
	}

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
		return shared.ErrReturnToMenu
	}

	for {
		ui.ClearScreen()
		printComposeProjects(projects)
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
		if err != nil || idx < 1 || idx > len(projects) {
			fmt.Println("无效编号")
			view.Pause()
			continue
		}

		project := projects[idx-1]
		if err := manageSingleProject(view, compose, project.Dir); err != nil {
			return err
		}
		// no auto refresh for projects list, return
		return shared.ErrReturnToMenu
	}
}

func manageSingleProject(view *ui.UI, compose update.ComposeCommand, dir string) error {
	fmt.Printf("\n项目目录: %s\n", dir)
	fmt.Println("请选择操作：")
	fmt.Println("1) Start/Up（compose up -d）")
	fmt.Println("2) Down")
	fmt.Println("3) Stop")
	fmt.Println("4) Restart")
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

	action, ok := composeProjectActionForChoice(ch)
	if !ok {
		fmt.Println("无效选项")
		view.Pause()
		return shared.ErrReturnToMenu
	}

	confirmed, _ := view.Confirm(fmt.Sprintf("确认执行 compose %s 于 %s ？(y/N): ", action.Name, dir))
	if !confirmed {
		fmt.Println("已取消")
		view.Pause()
		return shared.ErrReturnToMenu
	}

	if err := update.RunCompose(compose, dir, action.Args...); err != nil {
		log.Error("执行失败: ", err)
	} else {
		log.Info("已执行 compose ", action.Name, " 于 ", dir)
	}
	view.Pause()
	return shared.ErrReturnToMenu
}

func composeProjectActionForChoice(choice string) (composeProjectAction, bool) {
	switch choice {
	case "1":
		return composeProjectAction{Name: "up -d", Args: []string{"up", "-d"}}, true
	case "2":
		return composeProjectAction{Name: "down", Args: []string{"down"}}, true
	case "3":
		return composeProjectAction{Name: "stop", Args: []string{"stop"}}, true
	case "4":
		return composeProjectAction{Name: "restart", Args: []string{"restart"}}, true
	default:
		return composeProjectAction{}, false
	}
}
