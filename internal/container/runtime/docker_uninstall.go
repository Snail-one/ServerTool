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

var dockerDebUninstallPackages = []string{
	"docker-ce",
	"docker-ce-cli",
	"containerd.io",
	"docker-buildx-plugin",
	"docker-compose-plugin",
	"docker-ce-rootless-extras",
	"docker-scan-plugin",
	"docker.io",
	"docker-compose",
	"docker-engine",
}

var dockerRPMUninstallPackages = []string{
	"docker-ce",
	"docker-ce-cli",
	"containerd.io",
	"docker-buildx-plugin",
	"docker-compose-plugin",
	"docker-ce-rootless-extras",
	"docker-scan-plugin",
	"docker",
	"docker-client",
	"docker-client-latest",
	"docker-common",
	"docker-latest",
	"docker-latest-logrotate",
	"docker-logrotate",
	"docker-engine",
}

type dockerUninstallPlan struct {
	distribution dockerDistribution
	manager      string
	packages     []string
	repoFiles    []string
	removeData   bool
	dataPaths    []string
}

type dockerUninstaller struct {
	removeData    bool
	osReleasePath string
	readFile      func(string) ([]byte, error)
	commandExists func(string) bool
	output        func(string, ...string) (string, error)
	run           func(string, ...string) error
	fileExists    func(string) bool
	pathExists    func(string) bool
	removeFile    func(string) error
	removeTree    func(string) error
	confirm       func(dockerUninstallPlan) (bool, error)
}

type dockerUninstallPrompter interface {
	Ask(string) (string, error)
	Confirm(string) (bool, error)
}

func UninstallDocker(view *ui.UI) (bool, error) {
	if !system.IsRoot() {
		return false, fmt.Errorf("卸载 Docker 需要 root 权限，请使用 sudo 运行本工具")
	}
	removeData, proceed, err := selectDockerUninstallMode(view)
	if err != nil {
		return false, err
	}
	if !proceed {
		log.Info("已取消 Docker 卸载，未修改系统")
		return false, nil
	}
	return newDockerUninstaller(view, removeData).uninstall()
}

func selectDockerUninstallMode(view dockerUninstallPrompter) (bool, bool, error) {
	for {
		fmt.Println("请选择 Docker 卸载方式：")
		fmt.Println("1) 安全卸载（保留镜像、容器、卷和自定义配置）")
		fmt.Println("2) 彻底卸载（永久删除全部 Docker 数据和自定义配置）")
		fmt.Println("0/q) 返回")
		fmt.Println()
		choice, err := view.Ask("输入选项: ")
		if err != nil {
			return false, false, err
		}
		switch strings.ToLower(strings.TrimSpace(choice)) {
		case "1":
			return false, true, nil
		case "2":
			return true, true, nil
		case "0", "q", "exit":
			return false, false, nil
		default:
			fmt.Println("无效选项，请重新输入")
			fmt.Println()
		}
	}
}

func newDockerUninstaller(view *ui.UI, removeData bool) *dockerUninstaller {
	return &dockerUninstaller{
		osReleasePath: "/etc/os-release",
		readFile:      os.ReadFile,
		commandExists: system.CommandExists,
		output:        system.Output,
		run:           system.Run,
		fileExists: func(path string) bool {
			info, err := os.Lstat(path)
			return err == nil && info.Mode().IsRegular()
		},
		pathExists: func(path string) bool {
			_, err := os.Lstat(path)
			return err == nil
		},
		removeFile: shared.RemoveRegularFile,
		removeTree: shared.RemovePathTree,
		confirm:    func(plan dockerUninstallPlan) (bool, error) { return confirmDockerUninstall(view, plan) },
		removeData: removeData,
	}
}

func confirmDockerUninstall(view dockerUninstallPrompter, plan dockerUninstallPlan) (bool, error) {
	fmt.Println("即将卸载 Docker：")
	if len(plan.packages) > 0 {
		fmt.Println("软件包：")
		for _, name := range plan.packages {
			fmt.Println("- " + name)
		}
	}
	if len(plan.repoFiles) > 0 {
		fmt.Println("仓库配置和密钥：")
		for _, path := range plan.repoFiles {
			fmt.Println("- " + path)
		}
	}
	fmt.Println()
	if !plan.removeData {
		fmt.Println("以下数据和自定义配置将保留：")
		fmt.Println("- /var/lib/docker（镜像、容器、卷）")
		fmt.Println("- /var/lib/containerd")
		fmt.Println("- /etc/docker")
		fmt.Println("- Docker systemd 自定义配置")
		fmt.Println()
		return view.Confirm("确认安全卸载 Docker？请输入 y 确认，默认取消 (y/N): ")
	}
	fmt.Println("警告：以下路径将被永久递归删除，镜像、容器和卷无法恢复：")
	for _, path := range plan.dataPaths {
		fmt.Println("- " + path)
	}
	fmt.Println()
	answer, err := view.Ask("请输入 DELETE DOCKER DATA 确认彻底卸载: ")
	if err != nil {
		return false, err
	}
	return answer == "DELETE DOCKER DATA", nil
}

func (uninstaller *dockerUninstaller) uninstall() (bool, error) {
	plan, err := uninstaller.plan()
	if err != nil {
		return false, err
	}
	if len(plan.packages) == 0 && len(plan.repoFiles) == 0 && len(plan.dataPaths) == 0 {
		return false, fmt.Errorf("未检测到可由软件包管理器卸载的 Docker 或 Docker 官方仓库配置")
	}

	confirmed, err := uninstaller.confirm(plan)
	if err != nil {
		return false, fmt.Errorf("Docker 卸载在确认阶段失败: %w", err)
	}
	if !confirmed {
		log.Info("已取消 Docker 卸载，未修改系统")
		return false, nil
	}

	if len(plan.packages) > 0 || (plan.removeData && uninstaller.commandExists("docker")) {
		log.Info("[Docker 卸载/停止服务] systemctl disable --now docker.service docker.socket")
		if err := uninstaller.run("systemctl", "disable", "--now", "docker.service", "docker.socket"); err != nil {
			return false, fmt.Errorf("Docker 卸载在停止服务阶段失败: %w", err)
		}

	}

	if len(plan.packages) > 0 {
		log.Info("[Docker 卸载/软件包删除] ", strings.Join(plan.packages, ", "))
		args := []string{"remove", "-y"}
		if plan.distribution.Family == "apt" {
			args[0] = "purge"
		}
		args = append(args, plan.packages...)
		if err := uninstaller.run(plan.manager, args...); err != nil {
			return false, fmt.Errorf("Docker 卸载在软件包删除阶段失败: %w", err)
		}
	}

	for _, path := range plan.repoFiles {
		log.Info("[Docker 卸载/仓库清理] 删除 ", path)
		if err := uninstaller.removeFile(path); err != nil {
			return false, fmt.Errorf("Docker 卸载在仓库清理阶段失败: %w", err)
		}
	}
	if plan.removeData {
		for _, path := range plan.dataPaths {
			log.Info("[Docker 卸载/数据清理] 递归删除 ", path)
			if err := uninstaller.removeTree(path); err != nil {
				return false, fmt.Errorf("Docker 卸载在数据清理阶段失败: %w", err)
			}
		}
		log.Info("[Docker 卸载/systemd 刷新] systemctl daemon-reload")
		if err := uninstaller.run("systemctl", "daemon-reload"); err != nil {
			return false, fmt.Errorf("Docker 卸载在 systemd 刷新阶段失败: %w", err)
		}
		if err := uninstaller.run("systemctl", "reset-failed"); err != nil {
			return false, fmt.Errorf("Docker 卸载在 systemd 状态清理阶段失败: %w", err)
		}
	}

	if uninstaller.commandExists("docker") {
		version, versionErr := uninstaller.output("docker", "--version")
		if versionErr == nil {
			log.Warn("仍检测到不受上述软件包管理器管理的 Docker 命令：", strings.TrimSpace(version))
		}
	} else {
		log.Info("[Docker 卸载/结果验证] 已不再检测到 docker 命令")
	}
	if plan.removeData {
		log.Info("Docker 彻底卸载完成；软件包、仓库、镜像、容器、卷和自定义配置均已清理")
	} else {
		log.Info("Docker 安全卸载完成；镜像、容器、卷和自定义配置均已保留")
	}
	return true, nil
}

func (uninstaller *dockerUninstaller) plan() (dockerUninstallPlan, error) {
	detector := &dockerInstaller{osReleasePath: uninstaller.osReleasePath, readFile: uninstaller.readFile}
	distribution, err := detector.detectDistribution()
	if err != nil {
		return dockerUninstallPlan{}, fmt.Errorf("Docker 卸载在发行版识别阶段失败: %w", err)
	}
	managerDetector := &dockerInstaller{commandExists: uninstaller.commandExists}
	manager, err := managerDetector.packageManager(distribution)
	if err != nil {
		return dockerUninstallPlan{}, fmt.Errorf("Docker 卸载在包管理器识别阶段失败: %w", err)
	}

	packages, err := uninstaller.installedPackages(distribution)
	if err != nil {
		return dockerUninstallPlan{}, fmt.Errorf("Docker 卸载在软件包检测阶段失败: %w", err)
	}
	return dockerUninstallPlan{
		distribution: distribution,
		manager:      manager,
		packages:     packages,
		repoFiles:    uninstaller.existingRepositoryFiles(distribution),
		removeData:   uninstaller.removeData,
		dataPaths:    uninstaller.existingDataPaths(),
	}, nil
}

func (uninstaller *dockerUninstaller) installedPackages(distribution dockerDistribution) ([]string, error) {
	candidates := dockerRPMUninstallPackages
	queryName := "rpm"
	queryArgs := func(name string) []string { return []string{"-q", "--qf", "%{NAME}", name} }
	if distribution.Family == "apt" {
		candidates = dockerDebUninstallPackages
		queryName = "dpkg-query"
		queryArgs = func(name string) []string { return []string{"-W", "-f=${binary:Package}", name} }
	}
	if !uninstaller.commandExists(queryName) {
		return nil, fmt.Errorf("未找到 %s", queryName)
	}

	installed := make([]string, 0, len(candidates))
	for _, name := range candidates {
		output, err := uninstaller.output(queryName, queryArgs(name)...)
		if err == nil && strings.TrimSpace(output) != "" {
			installed = append(installed, strings.TrimSpace(output))
		}
	}
	sort.Strings(installed)
	return installed, nil
}

func (uninstaller *dockerUninstaller) existingRepositoryFiles(distribution dockerDistribution) []string {
	paths := []string{
		"/etc/yum.repos.d/docker-ce.repo",
		"/etc/pki/rpm-gpg/docker-ce.asc",
	}
	if distribution.Family == "apt" {
		paths = []string{
			"/etc/apt/sources.list.d/docker.sources",
			"/etc/apt/sources.list.d/docker.list",
			"/etc/apt/keyrings/docker.asc",
			"/etc/apt/keyrings/docker.gpg",
		}
	}
	existing := make([]string, 0, len(paths))
	for _, path := range paths {
		if uninstaller.fileExists(path) {
			existing = append(existing, path)
		}
	}
	return existing
}

func (uninstaller *dockerUninstaller) existingDataPaths() []string {
	if !uninstaller.removeData {
		return nil
	}
	paths := []string{
		"/var/lib/docker",
		"/var/lib/containerd",
		"/etc/docker",
		"/etc/systemd/system/docker.service.d",
		"/etc/systemd/system/docker.service",
		"/etc/systemd/system/docker.socket",
	}
	existing := make([]string, 0, len(paths))
	for _, path := range paths {
		if uninstaller.pathExists(path) {
			existing = append(existing, path)
		}
	}
	return existing
}
