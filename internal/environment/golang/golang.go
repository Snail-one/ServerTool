package golang

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

const (
	installRoot     = "/opt/go"
	currentLink     = "/opt/go/current"
	officialRoot    = "/usr/local/go"
	releasesURL     = "https://go.dev/dl/?mode=json&include=all"
	downloadBase    = "https://go.dev/dl/"
	pathBegin       = "# ===== BEGIN SNAIL GO ENVIRONMENT ====="
	pathEnd         = "# ===== END SNAIL GO ENVIRONMENT ====="
	pathBody        = `export PATH="/opt/go/current/bin:$PATH"`
	managedFile     = ".servertool-managed"
	versionPageSize = 10
)

type release struct {
	Version string        `json:"version"`
	Stable  bool          `json:"stable"`
	Files   []releaseFile `json:"files"`
}

type releaseFile struct {
	Filename string `json:"filename"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Version  string `json:"version"`
	SHA256   string `json:"sha256"`
	Kind     string `json:"kind"`
}

var apiClient = &http.Client{Timeout: 30 * time.Second}
var downloadClient = &http.Client{Timeout: 30 * time.Minute}

func Run(view *ui.UI) error {
	for {
		ui.ClearScreen()
		installed, err := installedVersions(installRoot)
		if err != nil {
			return err
		}
		active := activeVersion(currentLink)
		fmt.Println("Go 语言环境管理：")
		if active == "" {
			fmt.Println("当前版本：未配置")
		} else {
			fmt.Println("当前版本：" + active)
		}
		fmt.Printf("已安装版本：%d 个\n", len(installed))
		if officialMigrationDetected() {
			fmt.Println("检测到 /usr/local/go 或其 ~/.bashrc 环境变量 [安装或更新时可迁移]")
		}
		fmt.Println()
		fmt.Println("1) 安装 Go")
		fmt.Println("2) 更新到最新稳定版")
		fmt.Println("3) 切换当前版本")
		fmt.Println("4) 卸载 Go 版本")
		fmt.Println("5) 修复当前 Go（重新安装并修复 PATH）")
		fmt.Println("6) 清理 Go 安装残留")
		fmt.Println("0/q) 返回")
		fmt.Println()

		choice, err := view.Ask("输入选项: ")
		if err != nil {
			return err
		}
		fmt.Println()

		switch strings.ToLower(strings.TrimSpace(choice)) {
		case "1":
			shared.RunAction(view, "安装 Go 失败，已返回 Go 语言菜单", func() error {
				return installSelected(view)
			})
		case "2":
			shared.RunAction(view, "更新 Go 失败，已返回 Go 语言菜单", func() error {
				return updateLatest(view)
			})
		case "3":
			shared.RunAction(view, "切换 Go 版本失败，已返回 Go 语言菜单", func() error {
				return switchSelected(view)
			})
		case "4":
			shared.RunAction(view, "卸载 Go 版本失败，已返回 Go 语言菜单", func() error {
				return uninstallSelected(view)
			})
		case "5":
			shared.RunAction(view, "修复当前 Go 失败，已返回 Go 语言菜单", func() error {
				return repairCurrent(view)
			})
		case "6":
			shared.RunAction(view, "清理 Go 安装残留失败，已返回 Go 语言菜单", func() error {
				return cleanupInstallArtifacts(view)
			})
		case "0", "q", "exit":
			return shared.ErrReturnToMenu
		default:
			fmt.Println("无效选项，请重新输入")
			view.Pause()
		}
	}
}

func cleanupInstallArtifacts(view *ui.UI) error {
	artifacts, err := installArtifacts(installRoot)
	if err != nil {
		return err
	}
	if len(artifacts) == 0 {
		log.Info("未发现 Go 安装残留")
		return nil
	}
	fmt.Println("检测到以下 Go 安装残留：")
	containsBackup := false
	for _, name := range artifacts {
		fmt.Println("- " + filepath.Join(installRoot, name))
		if strings.HasPrefix(name, ".backup-") {
			containsBackup = true
		}
	}
	if containsBackup {
		log.Warn("备份目录可能包含异常中断前的旧版本，删除后无法通过该备份恢复")
	}
	confirmed, err := view.Confirm("确认删除以上安装残留？(y/N): ")
	if err != nil {
		return err
	}
	if !confirmed {
		log.Info("已取消清理")
		return nil
	}
	for _, name := range artifacts {
		path := filepath.Join(installRoot, name)
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("清理 Go 安装残留 %s 失败: %w", path, err)
		}
		log.Info("已清理：", path)
	}
	log.Info("Go 安装残留清理完成，共清理 ", len(artifacts), " 项")
	return nil
}

func installArtifacts(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取 Go 安装目录失败: %w", err)
	}
	artifacts := make([]string, 0)
	for _, entry := range entries {
		name := entry.Name()
		if name == ".current.tmp" ||
			strings.HasPrefix(name, ".backup-") ||
			strings.HasPrefix(name, ".repair-") ||
			strings.HasPrefix(name, ".install-") ||
			(strings.HasPrefix(name, ".download-") && strings.HasSuffix(name, ".tar.gz")) {
			artifacts = append(artifacts, name)
		}
	}
	sort.Strings(artifacts)
	return artifacts, nil
}

func repairCurrent(view *ui.UI) error {
	current := activeVersion(currentLink)
	if current == "" {
		return errors.New("当前没有可修复的工具管理 Go 版本，请先安装 Go")
	}
	confirmed, err := view.Confirm(fmt.Sprintf("将重新下载并替换当前版本 %s，同时修复 PATH，是否继续？(y/N): ", current))
	if err != nil {
		return err
	}
	if !confirmed {
		log.Info("已取消修复")
		return nil
	}
	removeOfficial, err := confirmOfficialInstallRemoval(view)
	if err != nil {
		return err
	}
	fileArch, err := supportedArch(runtime.GOARCH)
	if err != nil {
		return err
	}
	log.Info("检测运行平台：linux/", fileArch)
	releases, err := fetchReleases()
	if err != nil {
		return err
	}
	log.Info("查找当前版本的官方归档：", current)
	var selected release
	found := false
	for _, item := range availableReleases(releases, fileArch) {
		if item.Version == current {
			selected = item
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("Go 官方 API 中未找到当前版本 %s 的 linux/%s 归档", current, fileArch)
	}
	if err := reinstallRelease(selected); err != nil {
		return err
	}
	return finishOfficialInstallRemoval(removeOfficial)
}

func installSelected(view *ui.UI) error {
	removeOfficial, err := confirmOfficialInstallRemoval(view)
	if err != nil {
		return err
	}
	fileArch, err := supportedArch(runtime.GOARCH)
	if err != nil {
		return err
	}
	log.Info("检测运行平台：linux/", fileArch)
	releases, err := fetchReleases()
	if err != nil {
		return err
	}
	log.Info("筛选适用于 linux/", fileArch, " 的稳定归档...")
	available := availableReleases(releases, fileArch)
	if len(available) == 0 {
		return fmt.Errorf("Go 官方 API 未返回适用于 linux/%s 的稳定版本", runtime.GOARCH)
	}
	log.Info("筛选完成，可安装稳定版本：", len(available), " 个")
	fmt.Println()

	selected, err := selectRelease(view, available, runtime.GOARCH)
	if err != nil {
		return err
	}
	if err := installRelease(selected); err != nil {
		return err
	}
	return finishOfficialInstallRemoval(removeOfficial)
}

func selectRelease(view *ui.UI, releases []release, arch string) (release, error) {
	page := 0
	pageCount := (len(releases) + versionPageSize - 1) / versionPageSize
	for {
		start := page * versionPageSize
		end := start + versionPageSize
		if end > len(releases) {
			end = len(releases)
		}

		fmt.Printf("可安装的 Go 稳定版本（linux/%s，第 %d/%d 页，共 %d 个）：\n", arch, page+1, pageCount, len(releases))
		for i, item := range releases[start:end] {
			fmt.Printf("%d) %s\n", i+1, item.Version)
		}
		if page+1 < pageCount {
			fmt.Println("n) 下一页")
		}
		if page > 0 {
			fmt.Println("p) 上一页")
		}
		fmt.Println("0/q) 返回")
		fmt.Println()

		raw, err := view.Ask("选择版本: ")
		if err != nil {
			return release{}, err
		}
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "n":
			if page+1 < pageCount {
				page++
				fmt.Println()
				continue
			}
		case "p":
			if page > 0 {
				page--
				fmt.Println()
				continue
			}
		case "0", "q", "exit":
			return release{}, shared.ErrReturnToMenu
		default:
			index, parseErr := strconv.Atoi(raw)
			if parseErr == nil && index >= 1 && start+index <= end {
				return releases[start+index-1], nil
			}
		}
		fmt.Println("无效选项，请重新输入")
		fmt.Println()
	}
}

func updateLatest(view *ui.UI) error {
	removeOfficial, err := confirmOfficialInstallRemoval(view)
	if err != nil {
		return err
	}
	fileArch, err := supportedArch(runtime.GOARCH)
	if err != nil {
		return err
	}
	log.Info("检测运行平台：linux/", fileArch)
	releases, err := fetchReleases()
	if err != nil {
		return err
	}
	log.Info("筛选适用于 linux/", fileArch, " 的稳定归档...")
	available := availableReleases(releases, fileArch)
	if len(available) == 0 {
		return fmt.Errorf("Go 官方 API 未返回适用于 linux/%s 的稳定版本", runtime.GOARCH)
	}
	latest := available[0]
	log.Info("官方最新稳定版：", latest.Version)
	if activeVersion(currentLink) == latest.Version {
		log.Info("当前已是最新稳定版：", latest.Version)
		return finishOfficialInstallRemoval(removeOfficial)
	}
	if err := installRelease(latest); err != nil {
		return err
	}
	return finishOfficialInstallRemoval(removeOfficial)
}

func confirmOfficialInstallRemoval(view *ui.UI) (bool, error) {
	detected, err := officialMigrationState()
	if err != nil {
		return false, err
	}
	if !detected {
		return false, nil
	}
	confirmed, err := view.Confirm("检测到 /usr/local/go 或 ~/.bashrc 中的官方 Go 环境变量，是否清理并改用本工具安装？(y/N): ")
	if err != nil {
		return false, err
	}
	if !confirmed {
		log.Info("已取消操作，未修改 /usr/local/go 和 ~/.bashrc")
		return false, shared.ErrReturnToMenu
	}
	return true, nil
}

func finishOfficialInstallRemoval(remove bool) error {
	if !remove {
		return nil
	}
	return removeOfficialGoAndEnv()
}

func removeOfficialGoAndEnv() error {
	if err := removeOfficialInstall(officialRoot); err != nil {
		return err
	}
	account, err := system.CurrentTargetUser()
	if err != nil {
		return err
	}
	changed, err := cleanupOfficialGoBashrc(filepath.Join(account.Home, ".bashrc"))
	if err != nil {
		return err
	}
	if changed {
		if err := system.ChownPath(filepath.Join(account.Home, ".bashrc"), account, false); err != nil {
			return err
		}
		log.Info("已清理 ~/.bashrc 中引用 /usr/local/go 的 PATH 和 GOROOT")
	}
	log.Info("已完成官方位置 Go 卸载和环境清理")
	return nil
}

func officialMigrationDetected() bool {
	detected, err := officialMigrationState()
	return err == nil && detected
}

func officialMigrationState() (bool, error) {
	if officialInstallDetected(officialRoot) {
		return true, nil
	}
	account, err := system.CurrentTargetUser()
	if err != nil {
		return false, err
	}
	content, err := os.ReadFile(filepath.Join(account.Home, ".bashrc"))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return hasOfficialGoEnv(string(content)), nil
}

func hasOfficialGoEnv(content string) bool {
	_, changed := removeOfficialGoEnv(content)
	return changed
}

func cleanupOfficialGoBashrc(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	cleaned, changed := removeOfficialGoEnv(string(data))
	if !changed {
		return false, nil
	}
	if err := shared.AtomicWriteFile(path, []byte(cleaned), shared.AtomicWriteOptions{Mode: 0644}); err != nil {
		return false, err
	}
	return true, nil
}

func removeOfficialGoEnv(content string) (string, bool) {
	lines := strings.SplitAfter(content, "\n")
	kept := make([]string, 0, len(lines))
	changed := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		candidate := trimmed
		if len(candidate) > len("export") && strings.HasPrefix(candidate, "export") &&
			(candidate[len("export")] == ' ' || candidate[len("export")] == '\t') {
			candidate = strings.TrimSpace(candidate[len("export"):])
		}
		equals := strings.IndexByte(candidate, '=')
		variable := ""
		if equals >= 0 {
			variable = strings.TrimSpace(candidate[:equals])
		}
		isGoAssignment := variable == "PATH" || variable == "GOROOT"
		if !strings.HasPrefix(trimmed, "#") && isGoAssignment && strings.Contains(candidate, "/usr/local/go") {
			changed = true
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, ""), changed
}

func officialInstallDetected(root string) bool {
	return system.FileExists(filepath.Join(root, "bin", "go"))
}

func removeOfficialInstall(root string) error {
	if filepath.Clean(root) == string(os.PathSeparator) {
		return errors.New("拒绝删除根目录")
	}
	if !officialInstallDetected(root) {
		return nil
	}
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("卸载官方位置 Go 失败: %w", err)
	}
	return nil
}

func switchSelected(view *ui.UI) error {
	versions, err := installedVersions(installRoot)
	if err != nil {
		return err
	}
	selected, err := selectInstalled(view, versions, "选择要切换的版本: ")
	if err != nil {
		return err
	}
	if err := activateVersion(installRoot, currentLink, selected); err != nil {
		return err
	}
	if err := configureTargetUserPath(); err != nil {
		return err
	}
	log.Info("Go 当前版本已切换为：", selected)
	return nil
}

func uninstallSelected(view *ui.UI) error {
	versions, err := installedVersions(installRoot)
	if err != nil {
		return err
	}
	official, err := officialMigrationState()
	if err != nil {
		return err
	}
	if len(versions) == 0 && !official {
		return errors.New("未发现可卸载的 Go 安装")
	}

	fmt.Println("请选择要卸载的 Go：")
	offset := 0
	if official {
		fmt.Println("1) 官方位置 Go（/usr/local/go 及 ~/.bashrc 环境变量）")
		offset = 1
	}
	for i, version := range versions {
		fmt.Printf("%d) %s\n", i+1+offset, version)
	}
	fmt.Println("0/q) 返回")
	fmt.Println()
	raw, err := view.Ask("选择卸载项: ")
	if err != nil {
		return err
	}
	if shared.IsReturnChoice(raw) {
		return shared.ErrReturnToMenu
	}
	index, err := strconv.Atoi(raw)
	if err != nil || index < 1 || index > len(versions)+offset {
		return fmt.Errorf("无效的卸载选项：%s", raw)
	}
	if official && index == 1 {
		return uninstallOfficialGo(view)
	}
	selected := versions[index-1-offset]
	confirmed, err := view.Confirm(fmt.Sprintf("确认卸载 %s？(y/N): ", selected))
	if err != nil {
		return err
	}
	if !confirmed {
		log.Info("已取消卸载")
		return nil
	}

	wasActive := activeVersion(currentLink) == selected
	if err := os.RemoveAll(filepath.Join(installRoot, selected)); err != nil {
		return fmt.Errorf("删除 %s 失败: %w", selected, err)
	}
	log.Info("已卸载 Go 版本：", selected)
	if !wasActive {
		return nil
	}

	remaining, err := installedVersions(installRoot)
	if err != nil {
		return err
	}
	if len(remaining) > 0 {
		if err := activateVersion(installRoot, currentLink, remaining[0]); err != nil {
			return err
		}
		log.Info("已自动切换到：", remaining[0])
		return nil
	}
	if err := removeCurrentLink(currentLink); err != nil {
		return err
	}
	if err := cleanupTargetUserPath(); err != nil {
		return err
	}
	log.Info("已清理 Go PATH 配置")
	return nil
}

func uninstallOfficialGo(view *ui.UI) error {
	confirmed, err := view.Confirm("确认卸载 /usr/local/go 并清理 ~/.bashrc 中的官方 Go 环境变量？(y/N): ")
	if err != nil {
		return err
	}
	if !confirmed {
		log.Info("已取消卸载")
		return nil
	}
	return removeOfficialGoAndEnv()
}

func selectInstalled(view *ui.UI, versions []string, prompt string) (string, error) {
	if len(versions) == 0 {
		return "", errors.New("未发现由本工具管理的 Go 版本")
	}
	for i, version := range versions {
		fmt.Printf("%d) %s\n", i+1, version)
	}
	fmt.Println("0/q) 返回")
	fmt.Println()
	raw, err := view.Ask(prompt)
	if err != nil {
		return "", err
	}
	if shared.IsReturnChoice(raw) {
		return "", shared.ErrReturnToMenu
	}
	index, err := strconv.Atoi(raw)
	if err != nil || index < 1 || index > len(versions) {
		return "", fmt.Errorf("无效的版本选项：%s", raw)
	}
	return versions[index-1], nil
}

func installRelease(item release) error {
	log.Info("准备安装 Go 版本：", item.Version)
	fileArch, err := supportedArch(runtime.GOARCH)
	if err != nil {
		return err
	}
	archive, ok := archiveFor(item, fileArch)
	if !ok {
		return fmt.Errorf("%s 没有适用于 linux/%s 的官方归档", item.Version, runtime.GOARCH)
	}
	if err := os.MkdirAll(installRoot, 0755); err != nil {
		return fmt.Errorf("创建 Go 安装目录失败: %w", err)
	}
	destination := filepath.Join(installRoot, item.Version)
	if info, err := os.Stat(destination); err == nil && info.IsDir() {
		if !isManagedVersion(destination) {
			return fmt.Errorf("%s 已存在但不是本工具管理的有效 Go 版本目录，拒绝覆盖", destination)
		}
		log.Info(item.Version, " 已安装，直接切换")
	} else if err == nil {
		return fmt.Errorf("%s 已存在且不是目录，拒绝覆盖", destination)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	} else {
		if err := downloadAndInstall(archive, destination); err != nil {
			return err
		}
	}
	log.Info("切换当前版本软链接：", currentLink, " -> ", destination)
	if err := activateVersion(installRoot, currentLink, item.Version); err != nil {
		return err
	}
	log.Info("更新目标用户 ~/.bashrc 中的 Go PATH...")
	if err := configureTargetUserPath(); err != nil {
		return err
	}
	log.Info("Go PATH 配置完成")
	log.Info("Go 安装完成，当前版本：", item.Version)
	fmt.Println("重新登录或执行 source ~/.bashrc 后 PATH 生效")
	return nil
}

func reinstallRelease(item release) error {
	fileArch, err := supportedArch(runtime.GOARCH)
	if err != nil {
		return err
	}
	archive, ok := archiveFor(item, fileArch)
	if !ok {
		return fmt.Errorf("%s 没有适用于 linux/%s 的官方归档", item.Version, runtime.GOARCH)
	}
	destination := filepath.Join(installRoot, item.Version)
	if !isManagedVersion(destination) {
		return fmt.Errorf("当前版本目录不是本工具管理的有效 Go 版本：%s", destination)
	}
	replacement, err := unusedTempPath(installRoot, ".repair-*")
	if err != nil {
		return fmt.Errorf("创建修复临时路径失败: %w", err)
	}
	defer os.RemoveAll(replacement)
	log.Info("下载并准备当前版本的全新副本...")
	if err := downloadAndInstall(archive, replacement); err != nil {
		return err
	}
	log.Info("下载、校验和解压完成，开始替换当前版本目录...")
	if err := replaceManagedVersion(destination, replacement); err != nil {
		return err
	}
	log.Info("重新建立当前版本软链接：", currentLink, " -> ", destination)
	if err := activateVersion(installRoot, currentLink, item.Version); err != nil {
		return err
	}
	log.Info("重新写入目标用户 ~/.bashrc 中的 Go PATH...")
	if err := configureTargetUserPath(); err != nil {
		return err
	}
	log.Info("当前 Go 版本和 PATH 修复完成：", item.Version)
	fmt.Println("重新登录或执行 source ~/.bashrc 后 PATH 生效")
	return nil
}

func unusedTempPath(root, pattern string) (string, error) {
	path, err := os.MkdirTemp(root, pattern)
	if err != nil {
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return path, nil
}

func replaceManagedVersion(destination, replacement string) error {
	if !isManagedVersion(destination) || !isManagedVersion(replacement) {
		return errors.New("拒绝替换无效或非托管的 Go 版本目录")
	}
	backup, err := unusedTempPath(filepath.Dir(destination), ".backup-"+filepath.Base(destination)+"-*")
	if err != nil {
		return fmt.Errorf("创建版本备份路径失败: %w", err)
	}
	if err := os.Rename(destination, backup); err != nil {
		return fmt.Errorf("备份当前 Go 版本失败: %w", err)
	}
	if err := os.Rename(replacement, destination); err != nil {
		if rollbackErr := os.Rename(backup, destination); rollbackErr != nil {
			return fmt.Errorf("替换 Go 版本失败: %v；恢复原版本也失败: %w", err, rollbackErr)
		}
		return fmt.Errorf("替换 Go 版本失败，已恢复原版本: %w", err)
	}
	if err := os.RemoveAll(backup); err != nil {
		log.Warn("新版本已生效，但清理旧版本备份失败：", backup, "：", err)
	}
	return nil
}

func fetchReleases() ([]release, error) {
	log.Info("请求 Go 官方版本 API：", releasesURL)
	response, err := apiClient.Get(releasesURL)
	if err != nil {
		return nil, fmt.Errorf("请求 Go 官方版本 API 失败: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Go 官方版本 API 返回 HTTP %d", response.StatusCode)
	}
	log.Info("Go 官方版本 API 响应成功：", response.Status)
	log.Info("解析官方版本数据...")
	var releases []release
	if err := json.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(&releases); err != nil {
		return nil, fmt.Errorf("解析 Go 官方版本 API 失败: %w", err)
	}
	log.Info("官方版本数据解析完成，发行记录：", len(releases), " 条")
	return releases, nil
}

func availableReleases(input []release, arch string) []release {
	result := make([]release, 0, len(input))
	seen := make(map[string]bool)
	for _, item := range input {
		if !item.Stable || seen[item.Version] || !validVersion(item.Version) {
			continue
		}
		if _, ok := archiveFor(item, arch); !ok {
			continue
		}
		seen[item.Version] = true
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		return compareVersions(result[i].Version, result[j].Version) > 0
	})
	return result
}

func archiveFor(item release, arch string) (releaseFile, bool) {
	for _, file := range item.Files {
		if file.OS == "linux" && file.Arch == arch && file.Kind == "archive" && file.SHA256 != "" {
			return file, true
		}
	}
	return releaseFile{}, false
}

func supportedArch(arch string) (string, error) {
	switch arch {
	case "amd64", "arm64":
		return arch, nil
	default:
		return "", fmt.Errorf("暂不支持 Linux %s 架构，仅支持 amd64 和 arm64", arch)
	}
}

func downloadAndInstall(file releaseFile, destination string) error {
	log.Info("选定官方归档：", file.Filename)
	archivePath, err := downloadArchive(downloadBase+file.Filename, file.SHA256)
	if err != nil {
		return err
	}
	defer os.Remove(archivePath)

	staging, err := os.MkdirTemp(installRoot, ".install-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	log.Info("解压已校验归档到临时目录...")
	if err := extractGoArchive(archivePath, staging); err != nil {
		return fmt.Errorf("解压 Go 归档失败: %w", err)
	}
	extracted := filepath.Join(staging, "go")
	if info, err := os.Stat(filepath.Join(extracted, "bin", "go")); err != nil || info.IsDir() {
		return errors.New("Go 归档缺少 go/bin/go，已取消安装")
	}
	if err := os.WriteFile(filepath.Join(extracted, managedFile), []byte("managed by ServerTool\n"), 0644); err != nil {
		return fmt.Errorf("写入 Go 管理标记失败: %w", err)
	}
	log.Info("归档结构检查完成，写入版本目录：", destination)
	if err := os.Rename(extracted, destination); err != nil {
		return fmt.Errorf("写入 Go 版本目录失败: %w", err)
	}
	log.Info("版本目录写入完成")
	return nil
}

func downloadArchive(url, expectedSHA string) (string, error) {
	log.Info("访问官方下载地址：", url)
	response, err := downloadClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("下载 Go 归档失败: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载 Go 归档返回 HTTP %d", response.StatusCode)
	}
	log.Info("官方下载响应成功：", response.Status, "，开始接收文件...")
	file, err := os.CreateTemp(installRoot, ".download-*.tar.gz")
	if err != nil {
		return "", err
	}
	path := file.Name()
	keep := false
	defer func() {
		_ = file.Close()
		if !keep {
			_ = os.Remove(path)
		}
	}()
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(file, hash), response.Body)
	if err != nil {
		return "", fmt.Errorf("保存 Go 归档失败: %w", err)
	}
	log.Info("下载完成，接收字节数：", written)
	if err := file.Close(); err != nil {
		return "", err
	}
	log.Info("计算并核对官方 SHA-256...")
	actual := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(actual, expectedSHA) {
		return "", fmt.Errorf("Go 归档 SHA-256 校验失败：期望 %s，实际 %s", expectedSHA, actual)
	}
	log.Info("SHA-256 校验通过：", actual)
	keep = true
	return path, nil
}

func extractGoArchive(archivePath, destination string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	cleanRoot := filepath.Clean(destination) + string(os.PathSeparator)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(destination, filepath.Clean(header.Name))
		if !strings.HasPrefix(target, cleanRoot) {
			return fmt.Errorf("归档包含不安全路径：%s", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)&0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			output, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode)&0755)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(output, reader)
			closeErr := output.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		case tar.TypeSymlink:
			linkTarget := filepath.Clean(filepath.Join(filepath.Dir(target), header.Linkname))
			if !strings.HasPrefix(linkTarget, cleanRoot) {
				return fmt.Errorf("归档包含不安全软链接：%s", header.Name)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			if err := os.Symlink(header.Linkname, target); err != nil {
				return err
			}
		}
	}
}

func installedVersions(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取 Go 安装目录失败: %w", err)
	}
	versions := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() && validVersion(entry.Name()) && isManagedVersion(filepath.Join(root, entry.Name())) {
			versions = append(versions, entry.Name())
		}
	}
	sort.Slice(versions, func(i, j int) bool { return compareVersions(versions[i], versions[j]) > 0 })
	return versions, nil
}

func validVersion(version string) bool {
	parts, ok := versionParts(version)
	return ok && len(parts) >= 2
}

func versionParts(version string) ([]int, bool) {
	if !strings.HasPrefix(version, "go") {
		return nil, false
	}
	raw := strings.TrimPrefix(version, "go")
	pieces := strings.Split(raw, ".")
	if len(pieces) < 2 || len(pieces) > 3 {
		return nil, false
	}
	result := make([]int, len(pieces))
	for i, piece := range pieces {
		if piece == "" || (len(piece) > 1 && piece[0] == '0') {
			return nil, false
		}
		value, err := strconv.Atoi(piece)
		if err != nil || value < 0 {
			return nil, false
		}
		result[i] = value
	}
	return result, true
}

func compareVersions(left, right string) int {
	a, _ := versionParts(left)
	b, _ := versionParts(right)
	for len(a) < 3 {
		a = append(a, 0)
	}
	for len(b) < 3 {
		b = append(b, 0)
	}
	for i := 0; i < 3; i++ {
		if a[i] > b[i] {
			return 1
		}
		if a[i] < b[i] {
			return -1
		}
	}
	return 0
}

func activeVersion(link string) string {
	target, err := os.Readlink(link)
	if err != nil {
		return ""
	}
	version := filepath.Base(filepath.Clean(target))
	if !validVersion(version) || !isManagedVersion(link) {
		return ""
	}
	return version
}

func activateVersion(root, link, version string) error {
	if !validVersion(version) || !isManagedVersion(filepath.Join(root, version)) {
		return fmt.Errorf("Go 版本目录无效：%s", version)
	}
	if info, err := os.Lstat(link); err == nil && info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("%s 已存在且不是软链接，拒绝覆盖", link)
	} else if err == nil && !managedLinkPath(root, link) {
		return fmt.Errorf("%s 指向非托管路径，拒绝覆盖", link)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	temporary := filepath.Join(filepath.Dir(link), ".current.tmp")
	_ = os.Remove(temporary)
	if err := os.Symlink(filepath.Join(root, version), temporary); err != nil {
		return err
	}
	if err := os.Rename(temporary, link); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func managedLinkPath(root, link string) bool {
	target, err := os.Readlink(link)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(link), target)
	}
	target = filepath.Clean(target)
	return filepath.Dir(target) == filepath.Clean(root) && validVersion(filepath.Base(target))
}

func isManagedVersion(path string) bool {
	return system.FileExists(filepath.Join(path, "bin", "go")) &&
		system.FileExists(filepath.Join(path, managedFile))
}

func removeCurrentLink(link string) error {
	info, err := os.Lstat(link)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("%s 不是本工具可安全删除的软链接", link)
	}
	return os.Remove(link)
}

func configureTargetUserPath() error {
	account, err := system.CurrentTargetUser()
	if err != nil {
		return err
	}
	bashrc := filepath.Join(account.Home, ".bashrc")
	if err := shared.EnsureFileWithOptions(bashrc, shared.AtomicWriteOptions{
		Mode: 0644, Owner: &shared.FileOwner{UID: account.UID, GID: account.GID},
	}); err != nil {
		return err
	}
	if err := writeManagedPath(bashrc); err != nil {
		return err
	}
	return system.ChownPath(bashrc, account, false)
}

func writeManagedPath(bashrc string) error {
	data, err := os.ReadFile(bashrc)
	if err != nil {
		return err
	}
	content := shared.RemoveManagedBlock(string(data), pathBegin, pathEnd)
	block := shared.FormatManagedBlock(pathBegin, pathBody, pathEnd)
	if err := shared.AtomicWriteFile(bashrc, []byte(shared.AppendBlock(content, block)), shared.AtomicWriteOptions{Mode: 0644}); err != nil {
		return err
	}
	return nil
}

func cleanupTargetUserPath() error {
	account, err := system.CurrentTargetUser()
	if err != nil {
		return err
	}
	bashrc := filepath.Join(account.Home, ".bashrc")
	changed, err := cleanupManagedPath(bashrc)
	if err != nil || !changed {
		return err
	}
	return system.ChownPath(bashrc, account, false)
}

func cleanupManagedPath(bashrc string) (bool, error) {
	return shared.CleanupManagedBlocks(bashrc, shared.BlockMarker{Begin: pathBegin, End: pathEnd})
}
