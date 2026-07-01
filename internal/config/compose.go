package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	osuser "os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

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
	name             string
	args             []string
	display          string
	configFormatJSON bool
}

type dockerCleanupPlan struct {
	name         string
	args         []string
	needsConfirm bool
	skip         bool
}

type composeGitUpdater struct {
	account      *system.Account
	pulledRoots  map[string]struct{}
	gitAvailable bool
	warnedNoGit  bool
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

	account, accountErr := system.CurrentTargetUser()
	if accountErr != nil {
		log.Warn("无法获取目标用户，Git 源码拉取将使用当前用户：", accountErr)
	}
	gitUpdater := newComposeGitUpdater(account)

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
		ran, err := updateComposeDir(compose, gitUpdater, dir)
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

func updateComposeDir(compose composeCommand, gitUpdater *composeGitUpdater, dir string) (bool, error) {
	running, err := composeProjectRunning(compose, dir)
	if err != nil {
		return false, err
	}
	if !running {
		log.Info("跳过（未运行）: ", dir)
		return false, nil
	}

	log.Info("更新（运行中）: ", dir)
	if gitUpdater != nil {
		if err := gitUpdater.pull(dir); err != nil {
			log.Error("跳过（Git 拉取失败）: ", err)
			return false, nil
		}
	}

	hasBuild, err := composeProjectHasBuild(compose, dir)
	if err != nil {
		return true, err
	}
	if hasBuild {
		log.Info("检测到 build 配置，直接构建启动: ", dir)
		if err := runCompose(compose, dir, composeUpArgs(true)...); err != nil {
			log.Error("跳过（构建启动失败）: ", fmt.Errorf("%s up 失败: %w", dir, err))
			return false, nil
		}
		return true, nil
	}

	if err := runCompose(compose, dir, composePullArgs(compose)...); err != nil {
		return true, fmt.Errorf("%s pull 失败: %w", dir, err)
	}
	if err := runCompose(compose, dir, composeUpArgs(false)...); err != nil {
		return true, fmt.Errorf("%s up 失败: %w", dir, err)
	}
	return true, nil
}

func composePullArgs(compose composeCommand) []string {
	return []string{"pull"}
}

func composeUpArgs(build bool) []string {
	args := []string{"up", "-d"}
	if build {
		args = append(args, "--build")
	}
	return append(args, "--remove-orphans")
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

func newComposeGitUpdater(account *system.Account) *composeGitUpdater {
	return &composeGitUpdater{
		account:      account,
		pulledRoots:  make(map[string]struct{}),
		gitAvailable: system.CommandExists("git"),
	}
}

func (updater *composeGitUpdater) pull(dir string) error {
	if !updater.gitAvailable {
		if !updater.warnedNoGit {
			log.Warn("未找到 git，已跳过源码拉取")
			updater.warnedNoGit = true
		}
		return nil
	}

	account := updater.gitAccount(dir)
	root, ok, err := gitWorktreeRoot(account, dir)
	if err != nil {
		return err
	}
	if !ok {
		log.Info("跳过源码拉取（非 Git 工作区）: ", dir)
		return nil
	}

	root = filepath.Clean(root)
	if _, ok := updater.pulledRoots[root]; ok {
		return nil
	}

	account = updater.gitAccount(root)
	log.Info("拉取源码: ", root)
	if err := runGitPull(account, root); err != nil {
		return fmt.Errorf("%s git pull 失败: %w", root, err)
	}
	updater.pulledRoots[root] = struct{}{}
	return nil
}

func (updater *composeGitUpdater) gitAccount(path string) *system.Account {
	if account := accountForPathOwner(path); account != nil {
		return account
	}
	return updater.account
}

func accountForPathOwner(path string) *system.Account {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	account, err := lookupAccountByID(stat.Uid, stat.Gid)
	if err != nil {
		return nil
	}
	return account
}

func lookupAccountByID(uid, gid uint32) (*system.Account, error) {
	account, err := osuser.LookupId(strconv.FormatUint(uint64(uid), 10))
	if err != nil {
		return nil, err
	}
	primaryGID := gid
	if account.Gid != "" {
		parsedGID, err := strconv.ParseUint(account.Gid, 10, 32)
		if err == nil {
			primaryGID = uint32(parsedGID)
		}
	}
	if account.Username == "" || account.HomeDir == "" {
		return nil, fmt.Errorf("用户 %d 信息不完整", uid)
	}
	return &system.Account{
		Name: account.Username,
		Home: account.HomeDir,
		UID:  int(uid),
		GID:  int(primaryGID),
	}, nil
}

func gitWorktreeRoot(account *system.Account, dir string) (string, bool, error) {
	cmd := gitCommand(account, "-C", dir, "rev-parse", "--show-toplevel")
	output, err := cmd.CombinedOutput()
	if err != nil {
		if gitMarkerExists(dir) {
			return "", false, fmt.Errorf("%s Git 工作区检测失败: %s", dir, strings.TrimSpace(string(output)))
		}
		return "", false, nil
	}

	root := strings.TrimSpace(string(output))
	if root == "" {
		return "", false, nil
	}
	return root, true, nil
}

func gitMarkerExists(dir string) bool {
	current := filepath.Clean(dir)
	for {
		marker := filepath.Join(current, ".git")
		if isGitMarker(marker) {
			return true
		}

		parent := filepath.Dir(current)
		if parent == current {
			return false
		}
		current = parent
	}
}

func isGitMarker(marker string) bool {
	if system.DirExists(marker) {
		return system.FileExists(filepath.Join(marker, "HEAD"))
	}
	if !system.FileExists(marker) {
		return false
	}

	content, err := os.ReadFile(marker)
	if err != nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(string(content)), "gitdir:")
}

func runGitPull(account *system.Account, root string) error {
	cmd := gitCommand(account, "-C", root, "pull", "--ff-only")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func gitCommand(account *system.Account, args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	if account == nil {
		return cmd
	}

	cmd.Env = envWithAccount(os.Environ(), account)
	if os.Geteuid() == 0 && account.UID != 0 {
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: accountCredential(account)}
	}
	return cmd
}

func envWithAccount(base []string, account *system.Account) []string {
	overrides := map[string]string{
		"HOME":    account.Home,
		"LOGNAME": account.Name,
		"USER":    account.Name,
	}

	result := make([]string, 0, len(base)+len(overrides))
	for _, item := range base {
		key, _, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		if _, overridden := overrides[key]; overridden {
			continue
		}
		result = append(result, item)
	}
	for _, key := range []string{"HOME", "LOGNAME", "USER"} {
		result = append(result, key+"="+overrides[key])
	}
	return result
}

func accountCredential(account *system.Account) *syscall.Credential {
	credential := &syscall.Credential{
		Uid: uint32(account.UID),
		Gid: uint32(account.GID),
	}
	if groups := accountGroupIDs(account.Name); len(groups) > 0 {
		credential.Groups = groups
	}
	return credential
}

func accountGroupIDs(name string) []uint32 {
	account, err := osuser.Lookup(name)
	if err != nil {
		return nil
	}
	rawGroupIDs, err := account.GroupIds()
	if err != nil {
		return nil
	}

	groupIDs := make([]uint32, 0, len(rawGroupIDs))
	for _, rawGroupID := range rawGroupIDs {
		groupID, err := strconv.ParseUint(rawGroupID, 10, 32)
		if err != nil {
			continue
		}
		groupIDs = append(groupIDs, uint32(groupID))
	}
	return groupIDs
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
