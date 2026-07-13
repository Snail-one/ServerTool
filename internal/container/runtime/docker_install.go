package runtime

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

const (
	dockerDebFingerprint = "9DC858229FC7DD38854AE2D88D81803C0EBFCD88"
	dockerRPMFingerprint = "060A61C51B558A7F742B77AAC52FEB6B621E9F35"
)

var dockerPackages = []string{
	"docker-ce", "docker-ce-cli", "containerd.io", "docker-buildx-plugin", "docker-compose-plugin",
}

var dockerSupportedVersions = map[string]map[string]bool{
	"ubuntu": {"26.04": true, "25.10": true, "24.04": true, "22.04": true},
	"debian": {"13": true, "12": true, "11": true},
	"fedora": {"44": true, "43": true},
	"centos": {"10": true, "9": true},
	"rhel":   {"10": true, "9": true, "8": true},
}

var dockerSupportedArchitectures = map[string]map[string]bool{
	"ubuntu": {"amd64": true, "arm64": true, "armhf": true, "ppc64le": true, "s390x": true},
	"debian": {"amd64": true, "arm64": true, "armhf": true, "ppc64le": true},
	"fedora": {"amd64": true, "arm64": true, "ppc64le": true},
	"centos": {"amd64": true, "arm64": true, "ppc64le": true},
	"rhel":   {"amd64": true, "arm64": true, "s390x": true},
}

var dockerConflictPackages = map[string][]string{
	"debian": {"docker.io", "docker-compose", "docker-doc", "podman-docker", "containerd", "runc"},
	"ubuntu": {"docker.io", "docker-compose", "docker-compose-v2", "docker-doc", "podman-docker", "containerd", "runc"},
	"fedora": {
		"docker", "docker-client", "docker-client-latest", "docker-common", "docker-latest",
		"docker-latest-logrotate", "docker-logrotate", "docker-engine", "docker-selinux", "docker-engine-selinux",
	},
	"centos": {
		"docker", "docker-client", "docker-client-latest", "docker-common", "docker-latest",
		"docker-latest-logrotate", "docker-logrotate", "docker-engine",
	},
	"rhel": {
		"docker", "docker-client", "docker-client-latest", "docker-common", "docker-latest",
		"docker-latest-logrotate", "docker-logrotate", "docker-engine", "podman", "runc",
	},
}

type dockerDistribution struct {
	ID              string
	Name            string
	Version         string
	Codename        string
	VersionCodename string
	UbuntuCodename  string
	RepositorySuite string
	Architecture    string
	Family          string
}

type dockerFirewallReport struct {
	Warnings []string
}

type dockerInstallPlan struct {
	Distribution dockerDistribution
	Manager      string
	Dependencies []string
	Conflicts    []string
	Firewall     dockerFirewallReport
}

type dockerInstaller struct {
	osReleasePath   string
	readFile        func(string) ([]byte, error)
	readDir         func(string) ([]os.DirEntry, error)
	download        func(string, string) error
	run             func(string, ...string) error
	output          func(string, ...string) (string, error)
	commandExists   func(string) bool
	confirm         func([]string) (bool, error)
	confirmOnline   func() (bool, error)
	mkdirAll        func(string, os.FileMode) error
	writeFile       func(string, []byte, shared.AtomicWriteOptions) error
	platformCheck   func(*dockerDistribution) error
	dependencyCheck func(dockerDistribution) ([]string, error)
	firewallDetect  func() (dockerFirewallReport, error)
	repositoryCheck func(dockerDistribution) error
	onlineVerify    func() error
}

func newDockerInstaller(view *ui.UI) *dockerInstaller {
	installer := &dockerInstaller{
		osReleasePath: "/etc/os-release",
		readFile:      os.ReadFile,
		readDir:       os.ReadDir,
		download:      downloadDockerKey,
		run:           system.Run,
		output:        system.Output,
		commandExists: system.CommandExists,
		mkdirAll:      os.MkdirAll,
		writeFile:     shared.AtomicWriteFile,
	}
	installer.confirm = func(summary []string) (bool, error) {
		fmt.Println("Docker 官方 stable 仓库安装计划：")
		for _, line := range summary {
			fmt.Println("- " + line)
		}
		fmt.Println("取消将保持仓库、软件包和服务不变。")
		return view.Confirm("确认执行上述安装计划？(y/N): ")
	}
	installer.confirmOnline = func() (bool, error) {
		fmt.Println("本地 Docker Engine 与插件验证已通过。")
		fmt.Println("联网验证会从 Docker Hub 拉取并运行 hello-world 镜像。")
		return view.Confirm("是否执行联网/容器运行验证？(y/N): ")
	}
	installer.platformCheck = installer.validatePlatform
	installer.dependencyCheck = installer.missingDependencies
	installer.firewallDetect = installer.detectFirewall
	installer.repositoryCheck = installer.checkDuplicateRepositories
	installer.onlineVerify = func() error {
		return installer.run("docker", "run", "--rm", "hello-world")
	}
	return installer
}

func (installer *dockerInstaller) install() error {
	plan, err := installer.preflight()
	if err != nil {
		return err
	}

	confirmed, err := installer.confirm(installer.planSummary(plan))
	if err != nil {
		return fmt.Errorf("Docker 安装在总确认阶段失败: %w", err)
	}
	if !confirmed {
		log.Info("已取消 Docker 安装，未修改系统")
		return nil
	}

	if err := installer.installPrerequisites(plan); err != nil {
		return fmt.Errorf("Docker 安装在前置依赖阶段失败: %w", err)
	}

	keyURL, fingerprint := dockerKeyTrust(plan.Distribution)
	temp, err := os.CreateTemp("", "servertool-docker-gpg-*")
	if err != nil {
		return fmt.Errorf("Docker 安装在密钥下载阶段失败: %w", err)
	}
	tempPath := temp.Name()
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("Docker 安装在密钥下载阶段失败: %w", err)
	}
	defer os.Remove(tempPath)

	log.Info("[Docker 安装/密钥下载] 来源：", keyURL)
	if err := installer.download(keyURL, tempPath); err != nil {
		return fmt.Errorf("Docker 安装在密钥下载阶段失败: %w", err)
	}
	actual, err := installer.keyFingerprint(tempPath)
	if err != nil {
		return fmt.Errorf("Docker 安装在密钥验证阶段失败: %w", err)
	}
	fmt.Println("密钥实际指纹：" + formatFingerprint(actual))
	fmt.Println("代码信任指纹：" + formatFingerprint(fingerprint))
	if actual != fingerprint {
		return fmt.Errorf("Docker 安装在密钥验证阶段失败: 指纹不匹配（预期 %s，实际 %s），已拒绝配置仓库", fingerprint, actual)
	}
	log.Info("[Docker 安装/密钥验证] 完整主密钥指纹匹配")

	keyData, err := installer.readFile(tempPath)
	if err != nil {
		return fmt.Errorf("Docker 安装在仓库配置阶段失败: %w", err)
	}
	log.Info("[Docker 安装/仓库配置] 写入 Docker stable 签名仓库")
	if err := installer.configureRepository(plan.Distribution, keyData); err != nil {
		return fmt.Errorf("Docker 安装在仓库配置阶段失败: %w", err)
	}

	log.Info("[Docker 安装/元数据刷新] 验证 Docker 仓库并刷新软件包元数据")
	if err := installer.refreshMetadata(plan.Distribution); err != nil {
		return fmt.Errorf("Docker 安装在元数据刷新阶段失败: %w", err)
	}
	if len(plan.Conflicts) > 0 {
		log.Info("[Docker 安装/冲突包处理] 卸载已确认的冲突包：", strings.Join(plan.Conflicts, ", "))
		if err := installer.removeConflicts(plan.Distribution, plan.Conflicts); err != nil {
			return fmt.Errorf("Docker 安装在冲突包卸载阶段失败: %w", err)
		}
	}

	log.Info("[Docker 安装/软件包安装] 安装：", strings.Join(dockerPackages, ", "))
	if err := installer.installPackages(plan.Distribution); err != nil {
		if len(plan.Conflicts) > 0 {
			return fmt.Errorf("Docker 安装在软件包安装阶段失败: %w；已卸载的冲突包不会自动恢复，请使用系统软件源重新安装：%s", err, strings.Join(plan.Conflicts, " "))
		}
		return fmt.Errorf("Docker 安装在软件包安装阶段失败: %w", err)
	}

	log.Info("[Docker 安装/服务启动] systemctl enable --now docker")
	if err := installer.run("systemctl", "enable", "--now", "docker"); err != nil {
		return installer.serviceStartError(plan.Distribution, err)
	}
	if err := verifyDockerInstallation(installer.output); err != nil {
		return fmt.Errorf("Docker 安装在本地验证阶段失败: %w", err)
	}

	if installer.confirmOnline != nil {
		runOnline, err := installer.confirmOnline()
		if err != nil {
			return fmt.Errorf("Docker 安装在联网验证确认阶段失败: %w", err)
		}
		if runOnline {
			log.Info("[Docker 安装/联网验证] docker run --rm hello-world")
			if err := installer.onlineVerify(); err != nil {
				return fmt.Errorf("Docker 已安装且本地验证通过，但联网/容器运行验证失败: %w；不会回滚已完成的安装", err)
			}
		} else {
			log.Info("已跳过 hello-world 联网/容器运行验证")
		}
	}
	log.Info("Docker 安装完成")
	return nil
}

func (installer *dockerInstaller) preflight() (dockerInstallPlan, error) {
	log.Info("[Docker 安装/平台预检] 读取 ", installer.osReleasePath)
	distribution, err := installer.detectDistribution()
	if err != nil {
		return dockerInstallPlan{}, fmt.Errorf("Docker 安装在平台预检阶段失败: %w", err)
	}
	platformCheck := installer.platformCheck
	if platformCheck == nil {
		platformCheck = installer.validatePlatform
	}
	if err := platformCheck(&distribution); err != nil {
		return dockerInstallPlan{}, fmt.Errorf("Docker 安装在平台预检阶段失败: %w", err)
	}
	if !installer.commandExists("systemctl") {
		return dockerInstallPlan{}, fmt.Errorf("Docker 安装在平台预检阶段失败: 未找到 systemctl，无法按此安装方式管理 Docker 服务")
	}
	manager, err := installer.packageManager(distribution)
	if err != nil {
		return dockerInstallPlan{}, fmt.Errorf("Docker 安装在平台预检阶段失败: %w", err)
	}
	if err := installer.requiredQueryTool(distribution); err != nil {
		return dockerInstallPlan{}, fmt.Errorf("Docker 安装在平台预检阶段失败: %w", err)
	}
	repositoryCheck := installer.repositoryCheck
	if repositoryCheck == nil {
		repositoryCheck = installer.checkDuplicateRepositories
	}
	if err := repositoryCheck(distribution); err != nil {
		return dockerInstallPlan{}, fmt.Errorf("Docker 安装在重复仓库检查阶段失败: %w", err)
	}
	conflicts, err := installer.installedConflicts(distribution)
	if err != nil {
		return dockerInstallPlan{}, fmt.Errorf("Docker 安装在冲突包检测阶段失败: %w", err)
	}
	if distribution.ID == "rhel" && containsString(conflicts, "podman") {
		return dockerInstallPlan{}, fmt.Errorf("Docker 安装在冲突包检测阶段失败: 检测到 Podman；请先通过本工具现有的 Podman 卸载功能处理，安装器不会在后台自动删除 Podman")
	}
	dependencyCheck := installer.dependencyCheck
	if dependencyCheck == nil {
		dependencyCheck = installer.missingDependencies
	}
	dependencies, err := dependencyCheck(distribution)
	if err != nil {
		return dockerInstallPlan{}, fmt.Errorf("Docker 安装在前置依赖检查阶段失败: %w", err)
	}
	firewallDetect := installer.firewallDetect
	if firewallDetect == nil {
		firewallDetect = installer.detectFirewall
	}
	firewall, err := firewallDetect()
	if err != nil {
		return dockerInstallPlan{}, fmt.Errorf("Docker 安装在防火墙检查阶段失败: %w", err)
	}
	fmt.Printf("识别结果：%s %s（%s，%s）\n", distribution.Name, distribution.Version, distribution.Family, distribution.Architecture)
	return dockerInstallPlan{Distribution: distribution, Manager: manager, Dependencies: dependencies, Conflicts: conflicts, Firewall: firewall}, nil
}

func (installer *dockerInstaller) planSummary(plan dockerInstallPlan) []string {
	dist := plan.Distribution
	summary := []string{
		fmt.Sprintf("平台：%s %s / %s（官方支持策略）", dist.Name, dist.Version, dist.Architecture),
		fmt.Sprintf("目标仓库：https://download.docker.com/linux/%s（stable）", dist.ID),
		"目标软件包：" + strings.Join(dockerPackages, "、"),
	}
	if len(plan.Dependencies) == 0 {
		summary = append(summary, "前置依赖：已满足；仍会刷新系统软件包元数据")
	} else {
		summary = append(summary, "将安装前置依赖："+strings.Join(plan.Dependencies, "、"))
	}
	if len(plan.Conflicts) == 0 {
		summary = append(summary, "冲突包：未检测到已安装的冲突包")
	} else {
		summary = append(summary, "将卸载冲突包："+strings.Join(plan.Conflicts, "、"))
	}
	summary = append(summary, plan.Firewall.Warnings...)
	return summary
}

func (installer *dockerInstaller) detectDistribution() (dockerDistribution, error) {
	data, err := installer.readFile(installer.osReleasePath)
	if err != nil {
		return dockerDistribution{}, err
	}
	values := parseOSRelease(string(data))
	distribution := dockerDistribution{
		ID: strings.ToLower(values["ID"]), Name: values["PRETTY_NAME"], Version: values["VERSION_ID"],
		VersionCodename: values["VERSION_CODENAME"], UbuntuCodename: values["UBUNTU_CODENAME"],
	}
	if distribution.Name == "" {
		distribution.Name = values["NAME"]
	}
	if distribution.Name == "" {
		distribution.Name = distribution.ID
	}
	switch distribution.ID {
	case "debian":
		distribution.Family = "apt"
		distribution.RepositorySuite = distribution.VersionCodename
	case "ubuntu":
		distribution.Family = "apt"
		distribution.RepositorySuite = distribution.UbuntuCodename
		if distribution.RepositorySuite == "" {
			distribution.RepositorySuite = distribution.VersionCodename
		}
	case "fedora", "centos", "rhel":
		distribution.Family = "rpm"
	default:
		return dockerDistribution{}, fmt.Errorf("不支持的 Linux 发行版 %q，仅支持 Debian、Ubuntu、Fedora、CentOS Stream 和 RHEL；衍生发行版请使用对应上游说明或官方脚本", distribution.ID)
	}
	if distribution.Version == "" {
		return dockerDistribution{}, fmt.Errorf("%s 缺少 VERSION_ID", distribution.ID)
	}
	if distribution.Family == "apt" && distribution.RepositorySuite == "" {
		return dockerDistribution{}, fmt.Errorf("%s 缺少可用的仓库代号（VERSION_CODENAME/UBUNTU_CODENAME）", distribution.ID)
	}
	distribution.Codename = distribution.RepositorySuite
	return distribution, nil
}

func (installer *dockerInstaller) validatePlatform(distribution *dockerDistribution) error {
	if !dockerSupportedVersions[distribution.ID][distribution.Version] {
		return fmt.Errorf("Docker 官方 stable 仓库不支持 %s %s；支持版本：%s", distribution.ID, distribution.Version, strings.Join(sortedPolicyKeys(dockerSupportedVersions[distribution.ID]), "、"))
	}
	if distribution.ID == "centos" && !strings.Contains(strings.ToLower(distribution.Name), "stream") {
		return fmt.Errorf("仅支持 CentOS Stream 10/9，不支持 CentOS Linux 或无法确认 Stream 身份的平台")
	}
	rawArch, err := installer.output("uname", "-m")
	if err != nil {
		return fmt.Errorf("读取系统架构失败: %w", err)
	}
	architecture, ok := normalizeDockerArchitecture(strings.TrimSpace(rawArch))
	if !ok || !dockerSupportedArchitectures[distribution.ID][architecture] {
		return fmt.Errorf("Docker 官方仓库不支持 %s %s 的架构 %q", distribution.ID, distribution.Version, strings.TrimSpace(rawArch))
	}
	distribution.Architecture = architecture
	if distribution.ID == "centos" {
		manager, err := installer.packageManager(*distribution)
		if err != nil {
			return err
		}
		out, err := installer.output(manager, "repolist", "--enabled")
		if err != nil || !strings.Contains(strings.ToLower(out), "extras") {
			return fmt.Errorf("CentOS 的 centos-extras 仓库未启用或无法确认；请先用 dnf repolist --all 确认并启用 ID 含 extras 的仓库后重试")
		}
	}
	return nil
}

func parseOSRelease(content string) map[string]string {
	values := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[key] = strings.Trim(strings.TrimSpace(value), "\"'")
		}
	}
	return values
}

func normalizeDockerArchitecture(value string) (string, bool) {
	switch strings.ToLower(value) {
	case "x86_64", "amd64":
		return "amd64", true
	case "aarch64", "arm64":
		return "arm64", true
	case "armv7l", "armhf":
		return "armhf", true
	case "ppc64le":
		return "ppc64le", true
	case "s390x":
		return "s390x", true
	default:
		return "", false
	}
}

func sortedPolicyKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(keys)))
	return keys
}

func dockerKeyTrust(distribution dockerDistribution) (string, string) {
	if distribution.Family == "apt" {
		return "https://download.docker.com/linux/" + distribution.ID + "/gpg", dockerDebFingerprint
	}
	return "https://download.docker.com/linux/" + distribution.ID + "/gpg", dockerRPMFingerprint
}

func (installer *dockerInstaller) keyFingerprint(path string) (string, error) {
	output, err := installer.output("gpg", "--batch", "--show-keys", "--with-colons", path)
	if err != nil {
		return "", fmt.Errorf("gpg 无法读取下载的公钥: %w（输出：%s）", err, strings.TrimSpace(output))
	}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Split(line, ":")
		if len(fields) > 9 && fields[0] == "fpr" {
			return strings.ToUpper(fields[9]), nil
		}
	}
	return "", fmt.Errorf("下载内容中没有主密钥指纹")
}

func formatFingerprint(value string) string {
	var groups []string
	for len(value) > 4 {
		groups = append(groups, value[:4])
		value = value[4:]
	}
	if value != "" {
		groups = append(groups, value)
	}
	return strings.Join(groups, " ")
}

func (installer *dockerInstaller) configureRepository(distribution dockerDistribution, key []byte) error {
	if distribution.Family == "apt" {
		arch, err := installer.output("dpkg", "--print-architecture")
		if err != nil {
			return fmt.Errorf("读取 dpkg 架构失败: %w", err)
		}
		if err := installer.mkdirAll("/etc/apt/keyrings", 0755); err != nil {
			return err
		}
		keyPath := "/etc/apt/keyrings/docker.asc"
		if err := installer.writeFile(keyPath, key, shared.AtomicWriteOptions{Mode: 0644, ForceMode: true}); err != nil {
			return err
		}
		suite := distribution.RepositorySuite
		if suite == "" {
			suite = distribution.Codename
		}
		source := fmt.Sprintf("Types: deb\nURIs: https://download.docker.com/linux/%s\nSuites: %s\nComponents: stable\nArchitectures: %s\nSigned-By: %s\n", distribution.ID, suite, strings.TrimSpace(arch), keyPath)
		return installer.writeFile("/etc/apt/sources.list.d/docker.sources", []byte(source), shared.AtomicWriteOptions{Mode: 0644, ForceMode: true})
	}
	if err := installer.mkdirAll("/etc/pki/rpm-gpg", 0755); err != nil {
		return err
	}
	keyPath := "/etc/pki/rpm-gpg/docker-ce.asc"
	if err := installer.writeFile(keyPath, key, shared.AtomicWriteOptions{Mode: 0644, ForceMode: true}); err != nil {
		return err
	}
	if err := installer.mkdirAll("/etc/yum.repos.d", 0755); err != nil {
		return err
	}
	repo := fmt.Sprintf("[docker-ce-stable]\nname=Docker CE Stable - $basearch\nbaseurl=https://download.docker.com/linux/%s/$releasever/$basearch/stable\nenabled=1\ngpgcheck=1\nrepo_gpgcheck=0\ngpgkey=file://%s\n", distribution.ID, keyPath)
	return installer.writeFile("/etc/yum.repos.d/docker-ce.repo", []byte(repo), shared.AtomicWriteOptions{Mode: 0644, ForceMode: true})
}

func (installer *dockerInstaller) packageManager(distribution dockerDistribution) (string, error) {
	if distribution.Family == "apt" {
		if installer.commandExists("apt-get") {
			return "apt-get", nil
		}
		return "", fmt.Errorf("未找到 apt-get")
	}
	if installer.commandExists("dnf") {
		return "dnf", nil
	}
	if installer.commandExists("yum") {
		return "yum", nil
	}
	return "", fmt.Errorf("未找到 dnf 或 yum")
}

func (installer *dockerInstaller) requiredQueryTool(distribution dockerDistribution) error {
	if distribution.Family == "apt" && !installer.commandExists("dpkg-query") {
		return fmt.Errorf("未找到 dpkg-query，无法安全检测软件包状态")
	}
	if distribution.Family == "rpm" && !installer.commandExists("rpm") {
		return fmt.Errorf("未找到 rpm，无法安全检测软件包状态")
	}
	return nil
}

func (installer *dockerInstaller) installedConflicts(distribution dockerDistribution) ([]string, error) {
	var installed []string
	for _, name := range dockerConflictPackages[distribution.ID] {
		ok, err := installer.packageInstalled(distribution, name)
		if err != nil {
			return nil, err
		}
		if ok {
			installed = append(installed, name)
		}
	}
	sort.Strings(installed)
	return installed, nil
}

func (installer *dockerInstaller) packageInstalled(distribution dockerDistribution, name string) (bool, error) {
	if distribution.Family == "apt" {
		out, err := installer.output("dpkg-query", "-W", "-f=${db:Status-Abbrev} ${binary:Package}", name)
		if err != nil {
			return false, nil
		}
		fields := strings.Fields(out)
		return len(fields) >= 2 && fields[0] == "ii", nil
	}
	out, err := installer.output("rpm", "-q", "--qf", "%{NAME}", name)
	if err != nil {
		return false, nil
	}
	return strings.TrimSpace(out) == name, nil
}

func (installer *dockerInstaller) missingDependencies(distribution dockerDistribution) ([]string, error) {
	var missing []string
	caInstalled, err := installer.packageInstalled(distribution, "ca-certificates")
	if err != nil {
		return nil, fmt.Errorf("检查 ca-certificates 失败: %w", err)
	}
	if !caInstalled {
		missing = append(missing, "ca-certificates")
	}
	if distribution.Family == "apt" {
		gpgInstalled, err := installer.packageInstalled(distribution, "gnupg")
		if err != nil {
			return nil, fmt.Errorf("检查 gnupg 失败: %w", err)
		}
		if !gpgInstalled || !installer.commandExists("gpg") {
			missing = append(missing, "gnupg")
		}
	} else if !installer.commandExists("gpg") {
		missing = append(missing, "gnupg2")
	}
	return uniqueStrings(missing), nil
}

func (installer *dockerInstaller) installPrerequisites(plan dockerInstallPlan) error {
	if plan.Distribution.Family == "apt" {
		log.Info("[Docker 安装/前置依赖] apt-get update")
		if err := installer.run(plan.Manager, "update"); err != nil {
			return fmt.Errorf("刷新系统 APT 元数据失败: %w", err)
		}
	}
	if len(plan.Dependencies) > 0 {
		log.Info("[Docker 安装/前置依赖] 安装：", strings.Join(plan.Dependencies, ", "))
		args := append([]string{"install", "-y"}, plan.Dependencies...)
		if err := installer.run(plan.Manager, args...); err != nil {
			return fmt.Errorf("安装前置依赖 %s 失败: %w", strings.Join(plan.Dependencies, " "), err)
		}
	}
	if !installer.commandExists("gpg") {
		return fmt.Errorf("已处理依赖但仍未找到 gpg")
	}
	return nil
}

func (installer *dockerInstaller) detectFirewall() (dockerFirewallReport, error) {
	warnings := []string{
		"防火墙提示：Docker 发布端口可能绕过 ufw/firewalld 的常规入站规则",
		"自定义容器过滤规则应配置在 DOCKER-USER 链",
	}
	var detected []string
	for _, name := range []string{"ufw", "firewalld"} {
		if installer.commandExists(name) {
			detected = append(detected, name)
			continue
		}
		if installer.commandExists("systemctl") {
			if _, err := installer.output("systemctl", "is-active", name); err == nil {
				detected = append(detected, name)
			}
		}
	}
	if len(detected) > 0 {
		warnings = append(warnings, "检测到防火墙组件："+strings.Join(detected, "、"))
	}
	if installer.commandExists("nft") && len(detected) == 0 {
		out, err := installer.output("nft", "list", "ruleset")
		if err == nil && strings.TrimSpace(out) != "" {
			warnings = append(warnings, "兼容性警告：检测到原生 nftables 规则；Docker 规则集可能与其不兼容，请评估后继续")
		}
	}
	return dockerFirewallReport{Warnings: warnings}, nil
}

func (installer *dockerInstaller) checkDuplicateRepositories(distribution dockerDistribution) error {
	if distribution.Family == "apt" {
		if err := installer.checkRepositoryFile("/etc/apt/sources.list", distribution, ""); err != nil {
			return err
		}
		return installer.checkRepositoryDirectory("/etc/apt/sources.list.d", "docker.sources", distribution)
	}
	return installer.checkRepositoryDirectory("/etc/yum.repos.d", "docker-ce.repo", distribution)
}

func (installer *dockerInstaller) checkRepositoryDirectory(dir, managed string, distribution dockerDistribution) error {
	entries, err := installer.readDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == managed {
			continue
		}
		if err := installer.checkRepositoryFile(dir+"/"+entry.Name(), distribution, managed); err != nil {
			return err
		}
	}
	return nil
}

func (installer *dockerInstaller) checkRepositoryFile(path string, distribution dockerDistribution, _ string) error {
	data, err := installer.readFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	content := strings.ToLower(string(data))
	if strings.Contains(content, "download.docker.com/linux/"+distribution.ID) {
		return fmt.Errorf("检测到额外 Docker 仓库配置 %s；请先移除或合并重复配置，避免 Signed-By/仓库冲突", path)
	}
	return nil
}

func (installer *dockerInstaller) refreshMetadata(distribution dockerDistribution) error {
	manager, err := installer.packageManager(distribution)
	if err != nil {
		return err
	}
	if distribution.Family == "apt" {
		return installer.run(manager, "update")
	}
	return installer.run(manager, "makecache")
}

func (installer *dockerInstaller) removeConflicts(distribution dockerDistribution, packages []string) error {
	manager, err := installer.packageManager(distribution)
	if err != nil {
		return err
	}
	args := append([]string{"remove", "-y"}, packages...)
	return installer.run(manager, args...)
}

func (installer *dockerInstaller) installPackages(distribution dockerDistribution) error {
	manager, err := installer.packageManager(distribution)
	if err != nil {
		return err
	}
	args := append([]string{"install", "-y"}, dockerPackages...)
	return installer.run(manager, args...)
}

func (installer *dockerInstaller) serviceStartError(distribution dockerDistribution, startErr error) error {
	if distribution.ID == "fedora" {
		logs, _ := installer.output("journalctl", "-u", "docker", "--no-pager", "-n", "80")
		if strings.Contains(strings.ToLower(startErr.Error()+"\n"+logs), "failed to find iptables") {
			return fmt.Errorf("Docker 安装在服务启动阶段失败: %w；Fedora 检测到 failed to find iptables，请按官网提示执行 alternatives --set iptables /usr/bin/iptables-nft 后重试启动（本工具未自动修改 alternatives）", startErr)
		}
	}
	return fmt.Errorf("Docker 安装在服务启动阶段失败: %w", startErr)
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func downloadDockerKey(url, path string) error {
	return downloadDockerArtifact(url, path, 1024*1024)
}

func downloadDockerInstallScript(url, path string) error {
	return downloadDockerArtifact(url, path, 2*1024*1024)
}

func downloadDockerArtifact(url, path string, maxSize int64) error {
	client := &http.Client{Timeout: 30 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("下载返回 HTTP %s", response.Status)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, io.LimitReader(response.Body, maxSize+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() == 0 || info.Size() > maxSize {
		return fmt.Errorf("下载内容大小异常: %d 字节", info.Size())
	}
	return nil
}
