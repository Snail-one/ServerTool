package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"snail_tool/internal/log"
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

type composeCommand struct {
	name    string
	args    []string
	display string
}

type dockerCleanupPlan struct {
	name         string
	args         []string
	needsConfirm bool
	skip         bool
}

func UpdateDockerComposeApps(view *ui.UI) error {
	log.Info("批量更新运行中的 Docker Compose 应用")
	fmt.Println()

	defaultRoots := defaultComposeRoots()
	existingRoots, dirs, err := askComposeScanDirs(view, defaultRoots)
	if err != nil {
		return err
	}

	compose, err := detectComposeCommand()
	if err != nil {
		return err
	}

	fmt.Println("扫描目录：")
	for _, root := range existingRoots {
		fmt.Printf("- %s\n", root)
	}
	fmt.Printf("Compose 命令：%s\n", compose.display)
	fmt.Printf("找到 %d 个 Compose 目录：\n", len(dirs))
	for _, dir := range dirs {
		fmt.Printf("- %s\n", dir)
	}
	fmt.Println()

	confirmed, err := view.Confirm("将只更新运行中的项目，更新后可选择 Docker 清理策略，是否继续？(y/N): ")
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

	if err := runDockerCleanup(view); err != nil {
		return err
	}

	fmt.Println()
	log.Info("完成")
	fmt.Printf("已更新：%d，已跳过：%d\n", updated, skipped)
	return nil
}

func askComposeScanDirs(view *ui.UI, defaultRoots []string) ([]string, []string, error) {
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
		if isReturnChoice(rawRoots) {
			return nil, nil, ErrReturnToMenu
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

func runDockerCleanup(view *ui.UI) error {
	fmt.Println()
	log.Info("当前 Docker 磁盘占用")
	printDockerDiskUsage()
	fmt.Println()
	fmt.Println("请选择 Docker 清理策略：")
	fmt.Println("1) 清理悬空镜像（默认，docker image prune -f）")
	fmt.Println("2) 清理所有未被容器使用的镜像（docker image prune -a -f）")
	fmt.Println("3) 清理停止容器、无用网络、悬空镜像和构建缓存（docker system prune -f）")
	fmt.Println("4) 深度清理：停止容器、无用网络、所有未使用镜像和构建缓存（docker system prune -a -f）")
	fmt.Println("0/q) 跳过清理")
	fmt.Println()
	fmt.Println("说明：以上选项都不会清理 Docker volume。")
	fmt.Println()

	choice, err := view.Ask("输入选项（直接回车默认 1）: ")
	if err != nil {
		return err
	}
	fmt.Println()

	plan, err := dockerCleanupPlanForChoice(choice)
	if err != nil {
		fmt.Println(err)
		fmt.Println("已跳过 Docker 清理")
		return nil
	}
	if plan.skip {
		fmt.Println("已跳过 Docker 清理")
		return nil
	}

	if plan.needsConfirm {
		confirmed, err := view.Confirm(fmt.Sprintf("%s 可能需要后续重新拉取镜像，确认继续？(y/N): ", plan.name))
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Println("已取消 Docker 清理")
			return nil
		}
		fmt.Println()
	}

	log.Info(plan.name)
	if err := system.Run("docker", plan.args...); err != nil {
		return fmt.Errorf("Docker 清理失败: %w", err)
	}

	fmt.Println()
	log.Info("清理后 Docker 磁盘占用")
	printDockerDiskUsage()
	return nil
}

func dockerCleanupPlanForChoice(choice string) (dockerCleanupPlan, error) {
	switch strings.ToLower(strings.TrimSpace(choice)) {
	case "", "1":
		return dockerCleanupPlan{
			name: "清理悬空镜像",
			args: []string{"image", "prune", "-f"},
		}, nil
	case "2":
		return dockerCleanupPlan{
			name:         "清理所有未被容器使用的镜像",
			args:         []string{"image", "prune", "-a", "-f"},
			needsConfirm: true,
		}, nil
	case "3":
		return dockerCleanupPlan{
			name: "清理停止容器、无用网络、悬空镜像和构建缓存",
			args: []string{"system", "prune", "-f"},
		}, nil
	case "4":
		return dockerCleanupPlan{
			name:         "深度清理 Docker 无用资源",
			args:         []string{"system", "prune", "-a", "-f"},
			needsConfirm: true,
		}, nil
	case "0", "q", "exit":
		return dockerCleanupPlan{skip: true}, nil
	default:
		return dockerCleanupPlan{}, fmt.Errorf("无效 Docker 清理选项: %s", choice)
	}
}

func printDockerDiskUsage() {
	if err := system.Run("docker", "system", "df"); err != nil {
		log.Warn("无法获取 Docker 磁盘占用：", err)
	}
}

func defaultComposeRoots() []string {
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

func detectComposeCommand() (composeCommand, error) {
	if err := exec.Command("docker", "compose", "version").Run(); err == nil {
		return composeCommand{name: "docker", args: []string{"compose"}, display: "docker compose"}, nil
	}
	if system.CommandExists("docker-compose") {
		return composeCommand{name: "docker-compose", display: "docker-compose"}, nil
	}
	return composeCommand{}, fmt.Errorf("未找到 docker compose 或 docker-compose")
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

func updateComposeDir(compose composeCommand, dir string) (bool, error) {
	running, err := composeProjectRunning(compose, dir)
	if err != nil {
		return false, err
	}
	if !running {
		log.Info("跳过（未运行）: ", dir)
		return false, nil
	}

	log.Info("更新（运行中）: ", dir)
	if err := runCompose(compose, dir, "pull"); err != nil {
		return true, fmt.Errorf("%s pull 失败: %w", dir, err)
	}
	if err := runCompose(compose, dir, "up", "-d", "--remove-orphans"); err != nil {
		return true, fmt.Errorf("%s up 失败: %w", dir, err)
	}
	return true, nil
}

func composeProjectRunning(compose composeCommand, dir string) (bool, error) {
	output, err := composeOutput(compose, dir, "ps", "--status", "running", "-q")
	if err != nil {
		return false, nil
	}
	return strings.TrimSpace(output) != "", nil
}

func runCompose(compose composeCommand, dir string, args ...string) error {
	cmd := exec.Command(compose.name, composeArgs(compose, args...)...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func composeOutput(compose composeCommand, dir string, args ...string) (string, error) {
	cmd := exec.Command(compose.name, composeArgs(compose, args...)...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}

func composeArgs(compose composeCommand, args ...string) []string {
	allArgs := append([]string{}, compose.args...)
	return append(allArgs, args...)
}
