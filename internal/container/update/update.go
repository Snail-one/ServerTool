package update

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	containerruntime "snail_tool/internal/container/runtime"
	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

var defaultComposeRootCandidates = []string{
	"/docker",
	"/opt/docker",
	"/opt/apps",
}

var composeFilenames = map[string]struct{}{
	"docker-compose.yml":  {},
	"docker-compose.yaml": {},
	"compose.yml":         {},
	"compose.yaml":        {},
}

type ComposeCommand struct {
	Name             string
	Args             []string
	Display          string
	ConfigFormatJSON bool
}

type ComposeRebuildConfirmer interface {
	Confirm(prompt string) (bool, error)
}

type composeCommandCandidate struct {
	name    string
	args    []string
	display string
}

type composeLsProject struct {
	Name        string `json:"Name"`
	Status      string `json:"Status"`
	ConfigFiles string `json:"ConfigFiles"`
}

type composeRebuildResult struct {
	Dirs     []string
	Rebuilt  int
	Canceled bool
}

type composeRunner func(ComposeCommand, string, ...string) error

func Run(view *ui.UI) error {
	return UpdateDockerComposeApps(view)
}

func UpdateDockerComposeApps(view *ui.UI) error {
	log.Info("批量更新运行中的 Docker Compose 应用")
	fmt.Println()

	compose, err := DetectComposeCommand()
	if err != nil {
		return err
	}

	mode, err := chooseUpdateMode(view)
	if err != nil {
		return err
	}

	var scanRoots []string
	var dirs []string

	switch mode {
	case 1:
		// 运行中的项目更新（使用 docker compose ls，新默认）
		fmt.Println("正在通过 docker compose ls 获取运行中的项目...")
		dirs, err = GetComposeProjectDirsFromLS(compose)
		if err != nil {
			return err
		}
		if len(dirs) == 0 {
			log.Warn("未通过 compose ls 发现运行中的项目")
			view.Pause()
			return nil
		}
	case 2:
		// 扫描目录更新
		defaultRoots := DefaultComposeRoots()
		scanRoots, dirs, err = AskComposeScanDirs(view, defaultRoots)
		if err != nil {
			return err
		}
	default:
		return nil
	}

	if len(scanRoots) > 0 {
		fmt.Println("扫描目录：")
		for _, root := range scanRoots {
			fmt.Printf("- %s\n", root)
		}
	} else {
		fmt.Println("来源：docker compose ls")
	}
	fmt.Printf("Compose 命令：%s\n", compose.Display)
	fmt.Printf("找到 %d 个 Compose 目录：\n", len(dirs))
	for _, dir := range dirs {
		fmt.Printf("- %s\n", dir)
	}
	fmt.Println()

	confirmed, err := view.Confirm("将只更新运行中的项目，是否继续？(y/N): ")
	if err != nil {
		return err
	}
	if !confirmed {
		fmt.Println("已取消更新")
		return nil
	}

	updated, skipped := 0, 0
	for _, dir := range dirs {
		ran, err := updateComposeDir(compose, dir)
		if err != nil {
			return err
		}
		if ran {
			updated++
		} else {
			skipped++
		}
	}

	fmt.Println()
	log.Info("完成")
	fmt.Printf("已更新：%d，已跳过：%d\n", updated, skipped)
	return nil
}

func chooseUpdateMode(view *ui.UI) (int, error) {
	for {
		fmt.Println("请选择更新方式：")
		fmt.Println("1) 运行中的项目更新（默认）")
		fmt.Println("2) 扫描目录更新")
		fmt.Println("0/q) 返回")
		fmt.Println()

		raw, err := view.Ask("输入选项（直接回车默认 1）: ")
		if err != nil {
			return 0, err
		}
		fmt.Println()

		choice := strings.ToLower(strings.TrimSpace(raw))
		if shared.IsReturnChoice(choice) {
			return 0, shared.ErrReturnToMenu
		}

		switch choice {
		case "1", "":
			return 1, nil
		case "2":
			return 2, nil
		default:
			fmt.Println("无效选项，请重新输入")
			view.Pause()
		}
	}
}

func GetComposeProjectDirsFromLS(compose ComposeCommand) ([]string, error) {
	output, err := composeOutputGlobal(compose, "ls", "--format", "json")
	if err != nil {
		return nil, fmt.Errorf("执行 compose ls 失败: %w", err)
	}

	output = strings.TrimSpace(output)
	if output == "" || output == "[]" || output == "null" {
		return nil, nil
	}

	var projects []composeLsProject
	if err := json.Unmarshal([]byte(output), &projects); err != nil {
		return nil, fmt.Errorf("解析 compose ls 输出失败: %w (原始: %s)", err, output)
	}

	dirSet := make(map[string]struct{})
	for _, p := range projects {
		statusLower := strings.ToLower(p.Status)
		if !strings.Contains(statusLower, "running") {
			continue // 只处理运行中的项目
		}
		if p.ConfigFiles == "" {
			continue
		}

		// ConfigFiles 可能用逗号分隔多个文件
		for _, f := range strings.Split(p.ConfigFiles, ",") {
			f = strings.TrimSpace(f)
			if f == "" {
				continue
			}
			dir := filepath.Dir(f)
			dir = filepath.Clean(dir)
			if dir == "." || dir == "" {
				continue
			}
			dirSet[dir] = struct{}{}
		}
	}

	result := make([]string, 0, len(dirSet))
	for d := range dirSet {
		result = append(result, d)
	}
	sort.Strings(result)
	return result, nil
}

// GetAllComposeProjectDirsFromLS returns dirs from compose ls without the running filter (for management)
func GetAllComposeProjectDirsFromLS(compose ComposeCommand) ([]string, error) {
	output, err := composeOutputGlobal(compose, "ls", "--format", "json")
	if err != nil {
		return nil, fmt.Errorf("执行 compose ls 失败: %w", err)
	}

	output = strings.TrimSpace(output)
	if output == "" || output == "[]" || output == "null" {
		return nil, nil
	}

	var projects []composeLsProject
	if err := json.Unmarshal([]byte(output), &projects); err != nil {
		return nil, fmt.Errorf("解析 compose ls 输出失败: %w (原始: %s)", err, output)
	}

	dirSet := make(map[string]struct{})
	for _, p := range projects {
		if p.ConfigFiles == "" {
			continue
		}
		for _, f := range strings.Split(p.ConfigFiles, ",") {
			f = strings.TrimSpace(f)
			if f == "" {
				continue
			}
			dir := filepath.Dir(f)
			dir = filepath.Clean(dir)
			if dir == "." || dir == "" {
				continue
			}
			dirSet[dir] = struct{}{}
		}
	}

	result := make([]string, 0, len(dirSet))
	for d := range dirSet {
		result = append(result, d)
	}
	sort.Strings(result)
	return result, nil
}

func RebuildRunningComposeProjects(view *ui.UI) error {
	return RebuildRunningComposeProjectsWithPrompt(view, "确认按目录执行 compose down 后 compose up -d？(y/N): ")
}

func RebuildRunningComposeProjectsWithPrompt(confirmer ComposeRebuildConfirmer, confirmPrompt string) error {
	log.Info("批量重建运行中的 Docker Compose 项目")
	fmt.Println()

	compose, err := DetectComposeCommand()
	if err != nil {
		return err
	}

	fmt.Println("正在通过 docker compose ls 获取运行中的项目...")
	dirs, err := GetComposeProjectDirsFromLS(compose)
	if err != nil {
		return err
	}

	_, err = rebuildRunningComposeProjects(confirmer, compose, dirs, RunCompose, confirmPrompt)
	return err
}

func rebuildRunningComposeProjects(confirmer ComposeRebuildConfirmer, compose ComposeCommand, dirs []string, runner composeRunner, confirmPrompt string) (composeRebuildResult, error) {
	result := composeRebuildResult{Dirs: append([]string{}, dirs...)}
	if confirmPrompt == "" {
		confirmPrompt = "确认按目录执行 compose down 后 compose up -d？(y/N): "
	}

	fmt.Printf("Compose 命令：%s\n", compose.Display)
	fmt.Printf("找到 %d 个运行中的 Compose 项目：\n", len(dirs))
	for _, dir := range dirs {
		fmt.Printf("- %s\n", dir)
	}
	fmt.Println("说明：只会重建 Docker Compose 项目；非 Compose 创建的容器需要按原 docker run 参数手动重建。")
	fmt.Println()

	if len(dirs) == 0 {
		log.Warn("未发现运行中的 Compose 项目")
		return result, nil
	}

	confirmed, err := confirmer.Confirm(confirmPrompt)
	if err != nil {
		return result, err
	}
	if !confirmed {
		result.Canceled = true
		fmt.Println("已取消重建")
		fmt.Println("说明：已有容器通常需要重建后才会应用。")
		return result, nil
	}

	for _, dir := range dirs {
		log.Info("重建 Compose 项目：", dir)
		if err := runner(compose, dir, composeRebuildDownArgs()...); err != nil {
			return result, fmt.Errorf("%s down 失败: %w", dir, err)
		}
		if err := runner(compose, dir, composeRebuildUpArgs()...); err != nil {
			return result, fmt.Errorf("%s up -d 失败: %w", dir, err)
		}
		result.Rebuilt++
	}

	fmt.Println()
	log.Info("重建完成")
	fmt.Printf("已重建：%d\n", result.Rebuilt)
	return result, nil
}

func composeRebuildDownArgs() []string {
	return []string{"down"}
}

func composeRebuildUpArgs() []string {
	return []string{"up", "-d"}
}

func composeOutputGlobal(compose ComposeCommand, args ...string) (string, error) {
	cmd := exec.Command(compose.Name, composeArgs(compose, args...)...)
	out, err := cmd.Output()
	return string(out), err
}

func AskComposeScanDirs(view *ui.UI, defaultRoots []string) ([]string, []string, error) {
	for {
		fmt.Println("默认扫描目录：")
		for _, root := range defaultRoots {
			fmt.Printf("- %s\n", root)
		}
		fmt.Println()

		rawRoots, err := view.Ask("请输入扫描目录（直接回车使用默认目录，多个用空格或逗号分隔，0/q 返回）: ")
		if err != nil {
			return nil, nil, err
		}
		fmt.Println()
		if shared.IsReturnChoice(rawRoots) {
			return nil, nil, shared.ErrReturnToMenu
		}

		roots := parseComposeRoots(rawRoots, defaultRoots)
		existingRoots := filterExistingComposeRoots(roots)
		if len(existingRoots) == 0 {
			fmt.Println("没有可扫描的 Docker Compose 目录")
			view.PauseWithPrompt("按回车重新输入扫描目录...")
			continue
		}

		dirs, err := findComposeDirsInRoots(existingRoots)
		if err != nil {
			log.Warn("读取扫描目录失败：", err)
			view.PauseWithPrompt("按回车重新输入扫描目录...")
			continue
		}
		if len(dirs) == 0 {
			log.Warn("未找到 Docker Compose 配置文件")
			view.PauseWithPrompt("按回车重新输入扫描目录...")
			continue
		}

		return existingRoots, dirs, nil
	}
}

func DefaultComposeRoots() []string {
	roots := append([]string{}, defaultComposeRootCandidates...)
	if account, err := system.CurrentTargetUser(); err == nil {
		roots = append(roots, account.Home, filepath.Join(account.Home, "docker"))
	}
	return dedupeCleanPaths(roots)
}

func parseComposeRoots(raw string, defaultRoots []string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	if len(parts) == 0 {
		return defaultRoots
	}
	return dedupeCleanPaths(parts)
}

func filterExistingComposeRoots(roots []string) []string {
	existing := make([]string, 0, len(roots))
	for _, root := range roots {
		if system.DirExists(root) {
			existing = append(existing, root)
			continue
		}
		log.Warn("扫描目录不存在或不是目录，已跳过：", root)
	}
	return existing
}

func dedupeCleanPaths(paths []string) []string {
	result := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		cleaned := filepath.Clean(path)
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		result = append(result, cleaned)
	}
	return result
}

func DetectComposeCommand() (ComposeCommand, error) {
	if rt, ok := containerruntime.Detect(); ok {
		return DetectComposeCommandForRuntime(rt.Name)
	}
	return detectComposeCommand(composeCommandCandidatesForRuntime(""))
}

func DetectComposeCommandForRuntime(runtimeName string) (ComposeCommand, error) {
	compose, err := detectComposeCommand(composeCommandCandidatesForRuntime(runtimeName))
	if err == nil {
		return compose, nil
	}
	switch runtimeName {
	case "docker":
		return ComposeCommand{}, fmt.Errorf("未找到 docker compose 或 docker-compose")
	case "podman":
		return ComposeCommand{}, fmt.Errorf("未找到 podman compose 或 podman-compose")
	default:
		return ComposeCommand{}, err
	}
}

func detectComposeCommand(candidates []composeCommandCandidate) (ComposeCommand, error) {
	for _, candidate := range candidates {
		compose, ok := detectComposeCommandCandidate(candidate)
		if ok {
			return compose, nil
		}
	}
	return ComposeCommand{}, fmt.Errorf("未找到 docker compose / podman compose 或对应 legacy 工具")
}

func detectComposeCommandCandidate(candidate composeCommandCandidate) (ComposeCommand, bool) {
	if len(candidate.args) > 0 {
		args := append(append([]string{}, candidate.args...), "version")
		if err := exec.Command(candidate.name, args...).Run(); err != nil {
			return ComposeCommand{}, false
		}
	} else if !system.CommandExists(candidate.name) {
		return ComposeCommand{}, false
	}

	compose := ComposeCommand{Name: candidate.name, Args: candidate.args, Display: candidate.display}
	compose.ConfigFormatJSON = composeCommandSupports(compose, "config", "--format")
	return compose, true
}

func composeCommandCandidatesForRuntime(runtimeName string) []composeCommandCandidate {
	switch runtimeName {
	case "docker":
		return []composeCommandCandidate{
			{name: "docker", args: []string{"compose"}, display: "docker compose"},
			{name: "docker-compose", display: "docker-compose"},
		}
	case "podman":
		return []composeCommandCandidate{
			{name: "podman", args: []string{"compose"}, display: "podman compose"},
			{name: "podman-compose", display: "podman-compose"},
		}
	default:
		return []composeCommandCandidate{
			{name: "docker", args: []string{"compose"}, display: "docker compose"},
			{name: "docker-compose", display: "docker-compose"},
			{name: "podman", args: []string{"compose"}, display: "podman compose"},
			{name: "podman-compose", display: "podman-compose"},
		}
	}
}

func composeCommandSupports(compose ComposeCommand, command, option string) bool {
	output, err := exec.Command(compose.Name, composeArgs(compose, command, "--help")...).CombinedOutput()
	return err == nil && strings.Contains(string(output), option)
}

func findComposeDirsInRoots(roots []string) ([]string, error) {
	dirSet := make(map[string]struct{})
	for _, root := range roots {
		dirs, err := findComposeDirs(root)
		if err != nil {
			return nil, err
		}
		for _, dir := range dirs {
			dirSet[dir] = struct{}{}
		}
	}

	result := make([]string, 0, len(dirSet))
	for dir := range dirSet {
		result = append(result, dir)
	}
	sort.Strings(result)
	return result, nil
}

func findComposeDirs(root string) ([]string, error) {
	root = filepath.Clean(root)
	dirs := make(map[string]struct{})

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		depth, err := pathDepth(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if depth >= 2 {
				return filepath.SkipDir
			}
			return nil
		}
		if depth != 2 || !isRegularFile(entry) {
			return nil
		}

		if _, ok := composeFilenames[entry.Name()]; !ok {
			return nil
		}
		dirs[filepath.Dir(path)] = struct{}{}
		return nil
	})
	if err != nil {
		return nil, err
	}

	result := make([]string, 0, len(dirs))
	for dir := range dirs {
		result = append(result, dir)
	}
	sort.Strings(result)
	return result, nil
}

func pathDepth(root, path string) (int, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return 0, err
	}
	if rel == "." {
		return 0, nil
	}
	return strings.Count(rel, string(os.PathSeparator)) + 1, nil
}

func isRegularFile(entry os.DirEntry) bool {
	if entry.Type().IsRegular() {
		return true
	}
	info, err := entry.Info()
	return err == nil && info.Mode().IsRegular()
}

func updateComposeDir(compose ComposeCommand, dir string) (bool, error) {
	running, err := composeProjectRunning(compose, dir)
	if err != nil {
		return false, err
	}
	if !running {
		log.Info("跳过（未运行）: ", dir)
		return false, nil
	}

	log.Info("更新（运行中）: ", dir)

	hasBuild, err := composeProjectHasBuild(compose, dir)
	if err != nil {
		return true, err
	}
	if hasBuild {
		log.Info("跳过（需要构建）: ", dir)
		return false, nil
	}

	if err := RunCompose(compose, dir, composePullArgs(compose)...); err != nil {
		return true, fmt.Errorf("%s pull 失败: %w", dir, err)
	}
	if err := RunCompose(compose, dir, composeUpArgs()...); err != nil {
		return true, fmt.Errorf("%s up 失败: %w", dir, err)
	}
	return true, nil
}

func composePullArgs(compose ComposeCommand) []string {
	return []string{"pull"}
}

func composeUpArgs() []string {
	return []string{"up", "-d", "--remove-orphans"}
}

type composeConfig struct {
	Services map[string]composeServiceConfig `json:"services"`
}

type composeServiceConfig struct {
	Build json.RawMessage `json:"build"`
}

func composeProjectHasBuild(compose ComposeCommand, dir string) (bool, error) {
	if !compose.ConfigFormatJSON {
		log.Warn("Compose 不支持 config --format json，无法检测 build 配置：", dir)
		return false, nil
	}

	output, err := composeOutput(compose, dir, "config", "--format", "json")
	if err != nil {
		return false, fmt.Errorf("%s config 失败: %w", dir, err)
	}
	hasBuild, err := composeConfigHasBuild([]byte(output))
	if err != nil {
		return false, fmt.Errorf("%s config 解析失败: %w", dir, err)
	}
	return hasBuild, nil
}

func composeConfigHasBuild(raw []byte) (bool, error) {
	var config composeConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return false, err
	}

	for _, service := range config.Services {
		build := strings.TrimSpace(string(service.Build))
		if build != "" && build != "null" {
			return true, nil
		}
	}
	return false, nil
}

func composeProjectRunning(compose ComposeCommand, dir string) (bool, error) {
	output, err := composeOutput(compose, dir, "ps", "--status", "running", "-q")
	if err != nil {
		return false, nil
	}
	return strings.TrimSpace(output) != "", nil
}

func RunCompose(compose ComposeCommand, dir string, args ...string) error {
	cmd := exec.Command(compose.Name, composeArgs(compose, args...)...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func composeOutput(compose ComposeCommand, dir string, args ...string) (string, error) {
	cmd := exec.Command(compose.Name, composeArgs(compose, args...)...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}

func composeArgs(compose ComposeCommand, args ...string) []string {
	allArgs := append([]string{}, compose.Args...)
	return append(allArgs, args...)
}
