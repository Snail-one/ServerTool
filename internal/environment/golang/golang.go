package golang

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

const versionPageSize = 10

func Run(view *ui.UI) error {
	for {
		ui.ClearScreen()
		installed, err := installedVersions(installRoot)
		if err != nil {
			return err
		}
		active := activeVersion(currentLink)
		ui.MenuTitle("开发环境管理", "Go 语言")
		ui.PrintInfoCard("Go 环境状态", goStatusFields(active, len(installed), officialMigrationDetected())...)
		fmt.Println()
		ui.MenuOption("1", "安装 Go")
		ui.MenuOption("2", "更新到最新稳定版")
		ui.MenuOption("3", "切换当前版本")
		ui.MenuOptionHint("4", "创建 Go 命令软链接", "/usr/local/bin")
		ui.MenuOption("5", "卸载 Go 版本")
		ui.MenuOptionHint("6", "修复当前 Go", "重新安装并修复 PATH")
		ui.MenuOption("7", "清理 Go 安装残留")
		ui.MenuExit("0/q", "返回")
		fmt.Println()

		choice, err := view.Ask("请选择：")
		if err != nil {
			return err
		}
		fmt.Println()

		if shared.IsReturnChoice(choice) {
			return shared.ErrReturnToMenu
		}
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
			shared.RunAction(view, "创建 Go 命令软链接失败，已返回 Go 语言菜单", createGoCommandLinks)
		case "5":
			shared.RunAction(view, "卸载 Go 版本失败，已返回 Go 语言菜单", func() error {
				return uninstallSelected(view)
			})
		case "6":
			shared.RunAction(view, "修复当前 Go 失败，已返回 Go 语言菜单", func() error {
				return repairCurrent(view)
			})
		case "7":
			shared.RunAction(view, "清理 Go 安装残留失败，已返回 Go 语言菜单", func() error {
				return cleanupInstallArtifacts(view)
			})
		default:
			ui.InvalidChoice()
			view.Pause()
		}
	}
}

func goStatusFields(active string, installedCount int, official bool) []ui.CardField {
	fields := make([]ui.CardField, 0, 4)
	if active == "" {
		fields = append(fields, ui.CardField{Label: "当前版本", Value: ui.ConfiguredBadge(false)})
	} else {
		fields = append(fields, ui.CardField{Label: "当前版本", Value: ui.Badge(active, true)})
		fields = append(fields, ui.CardField{Label: "安装位置", Value: versionInstallPath(active)})
	}
	fields = append(fields, ui.CardField{Label: "已安装版本", Value: fmt.Sprintf("%d 个", installedCount)})
	if official {
		fields = append(fields, ui.CardField{
			Label:  "官方位置 Go",
			Value:  ui.StatusBadge("需确认"),
			Detail: officialRoot + " 或 ~/.bashrc 环境变量，可在安装或更新时迁移",
		})
	}
	return fields
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

		fmt.Println(ui.PrimaryBoldText(fmt.Sprintf("可安装的 Go 稳定版本（linux/%s，第 %d/%d 页，共 %d 个）：", arch, page+1, pageCount, len(releases))))
		for i, item := range releases[start:end] {
			ui.MenuOption(strconv.Itoa(i+1), item.Version)
		}
		if page+1 < pageCount {
			ui.MenuOption("n", "下一页")
		}
		if page > 0 {
			ui.MenuOption("p", "上一页")
		}
		ui.MenuExit("0/q", "返回")
		fmt.Println()

		raw, err := view.Ask("请选择版本：")
		if err != nil {
			return release{}, err
		}
		if shared.IsReturnChoice(raw) {
			return release{}, shared.ErrReturnToMenu
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
		default:
			index, parseErr := strconv.Atoi(raw)
			if parseErr == nil && index >= 1 && start+index <= end {
				return releases[start+index-1], nil
			}
		}
		ui.InvalidChoice()
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

func switchSelected(view *ui.UI) error {
	versions, err := installedVersions(installRoot)
	if err != nil {
		return err
	}
	selected, err := selectInstalled(view, versions, "请选择要切换的版本：")
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

func selectInstalled(view *ui.UI, versions []string, prompt string) (string, error) {
	if len(versions) == 0 {
		return "", errors.New("未发现由本工具管理的 Go 版本")
	}
	for {
		for i, version := range versions {
			ui.MenuOptionHint(strconv.Itoa(i+1), version, versionInstallPath(version))
		}
		ui.MenuExit("0/q", "返回")
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
			ui.InvalidChoice()
			fmt.Println()
			continue
		}
		return versions[index-1], nil
	}
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

	offset := 0
	if official {
		offset = 1
	}
	selected := ""
	for {
		ui.MenuSection("请选择要卸载的 Go")
		if official {
			ui.MenuOptionHint("1", "官方位置 Go", officialRoot)
		}
		for i, version := range versions {
			ui.MenuOptionHint(strconv.Itoa(i+1+offset), version, versionInstallPath(version))
		}
		ui.MenuExit("0/q", "返回")
		fmt.Println()
		raw, err := view.Ask("请选择卸载项：")
		if err != nil {
			return err
		}
		if shared.IsReturnChoice(raw) {
			return shared.ErrReturnToMenu
		}
		index, err := strconv.Atoi(raw)
		if err != nil || index < 1 || index > len(versions)+offset {
			ui.InvalidChoice()
			fmt.Println()
			continue
		}
		if official && index == 1 {
			return uninstallOfficialGo(view)
		}
		selected = versions[index-1-offset]
		break
	}
	confirmed, err := view.Confirm(fmt.Sprintf("确认卸载 %s？(y/N)：", selected))
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
	fields := []ui.CardField{{Label: "卸载版本", Value: selected}}
	if !wasActive {
		ui.PrintSuccessCard("Go 版本卸载完成", fields...)
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
		fields = append(fields, ui.CardField{Label: "当前版本", Value: remaining[0]})
		ui.PrintSuccessCard("Go 版本卸载完成", fields...)
		return nil
	}
	if err := removeCurrentLink(currentLink); err != nil {
		return err
	}
	if err := cleanupGoCommandLinks(commandBinDir, currentLink); err != nil {
		return err
	}
	if err := cleanupTargetUserPath(); err != nil {
		return err
	}
	log.Info("已清理 Go PATH 配置")
	fields = append(fields, ui.CardField{Label: "当前版本", Value: ui.ConfiguredBadge(false)})
	ui.PrintSuccessCard("Go 版本卸载完成", fields...)
	return nil
}

func uninstallOfficialGo(view *ui.UI) error {
	confirmed, err := view.Confirm("确认卸载 /usr/local/go 并清理 ~/.bashrc 中的官方 Go 环境变量？(y/N)：")
	if err != nil {
		return err
	}
	if !confirmed {
		log.Info("已取消卸载")
		return nil
	}
	return removeOfficialGoAndEnv()
}

func repairCurrent(view *ui.UI) error {
	current := activeVersion(currentLink)
	if current == "" {
		return errors.New("当前没有可修复的工具管理 Go 版本，请先安装 Go")
	}
	confirmed, err := view.Confirm(fmt.Sprintf("将重新下载并替换当前版本 %s，同时修复 PATH，是否继续？(y/N)：", current))
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

func cleanupInstallArtifacts(view *ui.UI) error {
	artifacts, err := installArtifacts(installRoot)
	if err != nil {
		return err
	}
	if len(artifacts) == 0 {
		log.Info("未发现 Go 安装残留")
		return nil
	}
	fmt.Println(ui.PrimaryBoldText("检测到以下 Go 安装残留："))
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
	confirmed, err := view.Confirm("确认删除以上安装残留？(y/N)：")
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
	ui.PrintSuccessCard("Go 安装残留清理完成",
		ui.CardField{Label: "清理数量", Value: fmt.Sprintf("%d 项", len(artifacts))},
		ui.CardField{Label: "安装根目录", Value: installRoot},
	)
	return nil
}
