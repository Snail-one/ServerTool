package runtime

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

var podmanUninstallPackages = []string{
	"podman",
	"podman-docker",
	"podman-compose",
	"podman-remote",
	"podman-machine",
}

type podmanUninstallPlan struct {
	manager     string
	family      string
	packages    []string
	removeData  bool
	targetUser  *system.Account
	configPaths []string
	resetRoot   bool
	resetUser   bool
}

type podmanUninstaller struct {
	removeData    bool
	commandExists func(string) bool
	output        func(string, ...string) (string, error)
	run           func(string, ...string) error
	runAs         func(*system.Account, string, ...string) error
	targetUser    func() (*system.Account, error)
	pathExists    func(string) bool
	removeTree    func(string) error
	confirm       func(podmanUninstallPlan) (bool, error)
}

func UninstallPodman(view *ui.UI) (bool, error) {
	if !system.IsRoot() {
		return false, fmt.Errorf("卸载 Podman 需要 root 权限，请使用 sudo 运行本工具")
	}
	removeData, proceed, err := selectPodmanUninstallMode(view)
	if err != nil {
		return false, err
	}
	if !proceed {
		log.Info("已取消 Podman 卸载，未修改系统")
		return false, nil
	}
	return newPodmanUninstaller(view, removeData).uninstall()
}

func selectPodmanUninstallMode(view dockerUninstallPrompter) (bool, bool, error) {
	for {
		fmt.Println("请选择 Podman 卸载方式：")
		fmt.Println("1) 卸载运行时（保留数据）")
		fmt.Println("2) 完全卸载（永久删除数据）")
		fmt.Println("0/q) 返回")
		fmt.Println()
		choice, err := view.Ask("输入选项: ")
		if err != nil {
			return false, false, err
		}
		if shared.IsReturnChoice(choice) {
			return false, false, nil
		}
		switch strings.ToLower(strings.TrimSpace(choice)) {
		case "1":
			return false, true, nil
		case "2":
			return true, true, nil
		default:
			fmt.Println("无效选项，请重新输入")
			fmt.Println()
		}
	}
}

func newPodmanUninstaller(view *ui.UI, removeData bool) *podmanUninstaller {
	return &podmanUninstaller{
		removeData:    removeData,
		commandExists: system.CommandExists,
		output:        system.Output,
		run:           system.Run,
		runAs:         runPodmanAsAccount,
		targetUser:    system.CurrentTargetUser,
		pathExists: func(path string) bool {
			_, err := os.Lstat(path)
			return err == nil
		},
		removeTree: shared.RemovePathTree,
		confirm:    func(plan podmanUninstallPlan) (bool, error) { return confirmPodmanUninstall(view, plan) },
	}
}

func runPodmanAsAccount(account *system.Account, name string, args ...string) error {
	runArgs := []string{"-u", account.Name, "--", "env", "HOME=" + account.Home}
	runtimeDir := fmt.Sprintf("/run/user/%d", account.UID)
	if system.DirExists(runtimeDir) {
		runArgs = append(runArgs, "XDG_RUNTIME_DIR="+runtimeDir)
	}
	runArgs = append(runArgs, name)
	runArgs = append(runArgs, args...)
	return system.Run("runuser", runArgs...)
}

func confirmPodmanUninstall(view dockerUninstallPrompter, plan podmanUninstallPlan) (bool, error) {
	fmt.Println("即将卸载 Podman：")
	if len(plan.packages) > 0 {
		fmt.Println("软件包：")
		for _, name := range plan.packages {
			fmt.Println("- " + name)
		}
	}
	fmt.Println()
	if !plan.removeData {
		fmt.Println("Podman 的 rootful/rootless 容器数据和用户配置将保留。")
		fmt.Println()
		return view.Confirm("确认卸载 Podman 运行时并保留数据？请输入 y 确认，默认取消 (y/N): ")
	}

	fmt.Println("警告：Podman system reset 将永久删除 rootful 和当前用户的：")
	fmt.Println("- pods、容器、镜像、网络、卷、构建缓存")
	fmt.Println("- Podman graphRoot 和 runRoot 存储目录")
	if len(plan.configPaths) > 0 {
		fmt.Println("以下用户配置目录也将被永久删除：")
		for _, path := range plan.configPaths {
			fmt.Println("- " + path)
		}
	}
	fmt.Println("注意：Podman 与 Buildah 等工具可能共享容器存储。")
	fmt.Println()
	answer, err := view.Ask("请输入 DELETE PODMAN DATA 确认完全卸载: ")
	if err != nil {
		return false, err
	}
	return answer == "DELETE PODMAN DATA", nil
}

func (uninstaller *podmanUninstaller) uninstall() (bool, error) {
	plan, err := uninstaller.plan()
	if err != nil {
		return false, err
	}
	if len(plan.packages) == 0 && !plan.resetRoot && !plan.resetUser && len(plan.configPaths) == 0 {
		return false, fmt.Errorf("未检测到可由软件包管理器卸载的 Podman")
	}

	confirmed, err := uninstaller.confirm(plan)
	if err != nil {
		return false, fmt.Errorf("Podman 卸载在确认阶段失败: %w", err)
	}
	if !confirmed {
		log.Info("已取消 Podman 卸载，未修改系统")
		return false, nil
	}

	if plan.removeData && plan.resetRoot {
		log.Info("[Podman 卸载/rootful 数据清理] podman system reset --force")
		if err := uninstaller.run("podman", "system", "reset", "--force"); err != nil {
			return false, fmt.Errorf("Podman 卸载在 rootful 数据清理阶段失败: %w", err)
		}
	}
	if plan.removeData && plan.resetUser {
		log.Info("[Podman 卸载/rootless 数据清理] 用户 ", plan.targetUser.Name, "：podman system reset --force")
		if err := uninstaller.runAs(plan.targetUser, "podman", "system", "reset", "--force"); err != nil {
			return false, fmt.Errorf("Podman 卸载在用户 %s 的 rootless 数据清理阶段失败: %w", plan.targetUser.Name, err)
		}
	}

	if len(plan.packages) > 0 {
		log.Info("[Podman 卸载/软件包删除] ", strings.Join(plan.packages, ", "))
		operation := "remove"
		if plan.family == "apt" {
			operation = "purge"
		}
		args := append([]string{operation, "-y"}, plan.packages...)
		if err := uninstaller.run(plan.manager, args...); err != nil {
			return false, fmt.Errorf("Podman 卸载在软件包删除阶段失败: %w", err)
		}
	}

	if plan.removeData {
		for _, path := range plan.configPaths {
			log.Info("[Podman 卸载/用户配置清理] 递归删除 ", path)
			if err := uninstaller.removeTree(path); err != nil {
				return false, fmt.Errorf("Podman 卸载在用户配置清理阶段失败: %w", err)
			}
		}
	}

	if uninstaller.commandExists("podman") {
		version, versionErr := uninstaller.output("podman", "--version")
		if versionErr == nil {
			log.Warn("仍检测到不受上述软件包管理器管理的 Podman 命令：", strings.TrimSpace(version))
		}
	} else {
		log.Info("[Podman 卸载/结果验证] 已不再检测到 podman 命令")
	}
	if plan.removeData {
		log.Info("Podman 完全卸载完成；Podman 软件包、容器数据和当前用户配置均已清理")
	} else {
		log.Info("Podman 运行时卸载完成；Podman 容器数据和用户配置均已保留")
	}
	return true, nil
}

func (uninstaller *podmanUninstaller) plan() (podmanUninstallPlan, error) {
	manager, family, queryName, err := podmanPackageTools(uninstaller.commandExists)
	if err != nil {
		return podmanUninstallPlan{}, fmt.Errorf("Podman 卸载在包管理器识别阶段失败: %w", err)
	}
	packages := uninstaller.installedPackages(family, queryName)
	plan := podmanUninstallPlan{
		manager:    manager,
		family:     family,
		packages:   packages,
		removeData: uninstaller.removeData,
		resetRoot:  uninstaller.removeData && uninstaller.commandExists("podman"),
	}
	if !uninstaller.removeData {
		return plan, nil
	}

	account, err := uninstaller.targetUser()
	if err != nil {
		return podmanUninstallPlan{}, fmt.Errorf("无法确定需要清理 rootless Podman 数据的目标用户: %w", err)
	}
	plan.targetUser = account
	if account.UID != 0 {
		if !uninstaller.commandExists("runuser") {
			return podmanUninstallPlan{}, fmt.Errorf("未找到 runuser，无法安全清理用户 %s 的 rootless Podman 数据", account.Name)
		}
		plan.resetUser = uninstaller.commandExists("podman")
	}
	plan.configPaths = uninstaller.existingConfigPaths(account)
	return plan, nil
}

func podmanPackageTools(commandExists func(string) bool) (string, string, string, error) {
	if commandExists("dpkg-query") {
		if commandExists("apt-get") {
			return "apt-get", "apt", "dpkg-query", nil
		}
		if commandExists("apt") {
			return "apt", "apt", "dpkg-query", nil
		}
	}
	if commandExists("rpm") {
		if commandExists("dnf") {
			return "dnf", "rpm", "rpm", nil
		}
		if commandExists("yum") {
			return "yum", "rpm", "rpm", nil
		}
	}
	return "", "", "", fmt.Errorf("未找到受支持的软件包管理器（apt、dnf 或 yum）")
}

func (uninstaller *podmanUninstaller) installedPackages(family, queryName string) []string {
	var installed []string
	for _, name := range podmanUninstallPackages {
		var output string
		var err error
		if family == "apt" {
			output, err = uninstaller.output(queryName, "-W", "-f=${binary:Package}", name)
		} else {
			output, err = uninstaller.output(queryName, "-q", "--qf", "%{NAME}", name)
		}
		if err == nil && strings.TrimSpace(output) != "" {
			installed = append(installed, strings.TrimSpace(output))
		}
	}
	sort.Strings(installed)
	return installed
}

func (uninstaller *podmanUninstaller) existingConfigPaths(account *system.Account) []string {
	paths := []string{"/root/.config/containers"}
	if account.UID != 0 {
		paths = append(paths, account.Home+"/.config/containers")
	}
	var existing []string
	for _, path := range paths {
		if uninstaller.pathExists(path) {
			existing = append(existing, path)
		}
	}
	return existing
}
