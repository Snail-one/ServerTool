package update

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

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

type composeCommand struct {
	name             string
	args             []string
	display          string
	configFormatJSON bool
}

func Run(view *ui.UI) error {
	return UpdateDockerComposeApps(view)
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
		compose := composeCommand{name: "docker", args: []string{"compose"}, display: "docker compose"}
		compose.configFormatJSON = composeCommandSupports(compose, "config", "--format")
		return compose, nil
	}
	if system.CommandExists("docker-compose") {
		compose := composeCommand{name: "docker-compose", display: "docker-compose"}
		compose.configFormatJSON = composeCommandSupports(compose, "config", "--format")
		return compose, nil
	}
	return composeCommand{}, fmt.Errorf("未找到 docker compose 或 docker-compose")
}

func composeCommandSupports(compose composeCommand, command, option string) bool {
	output, err := exec.Command(compose.name, composeArgs(compose, command, "--help")...).CombinedOutput()
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

	hasBuild, err := composeProjectHasBuild(compose, dir)
	if err != nil {
		return true, err
	}
	if hasBuild {
		log.Info("跳过（需要构建）: ", dir)
		return false, nil
	}

	if err := runCompose(compose, dir, composePullArgs(compose)...); err != nil {
		return true, fmt.Errorf("%s pull 失败: %w", dir, err)
	}
	if err := runCompose(compose, dir, composeUpArgs()...); err != nil {
		return true, fmt.Errorf("%s up 失败: %w", dir, err)
	}
	return true, nil
}

func composePullArgs(compose composeCommand) []string {
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

func composeProjectHasBuild(compose composeCommand, dir string) (bool, error) {
	if !compose.configFormatJSON {
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
