package runtime

import (
	"bufio"
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

type dockerDistribution struct {
	ID       string
	Name     string
	Version  string
	Codename string
	Family   string
}

type dockerInstaller struct {
	osReleasePath string
	readFile      func(string) ([]byte, error)
	download      func(string, string) error
	run           func(string, ...string) error
	output        func(string, ...string) (string, error)
	commandExists func(string) bool
	confirm       func([]string) (bool, error)
	mkdirAll      func(string, os.FileMode) error
	writeFile     func(string, []byte, shared.AtomicWriteOptions) error
}

func newDockerInstaller(view *ui.UI) *dockerInstaller {
	return &dockerInstaller{
		osReleasePath: "/etc/os-release",
		readFile:      os.ReadFile,
		download:      downloadDockerKey,
		run:           system.Run,
		output:        system.Output,
		commandExists: system.CommandExists,
		confirm: func(packages []string) (bool, error) {
			fmt.Println("检测到与 Docker CE 冲突的软件包：")
			for _, name := range packages {
				fmt.Println("- " + name)
			}
			fmt.Println("取消将保持系统不变。")
			return view.Confirm("是否卸载这些冲突包并继续？(y/N): ")
		},
		mkdirAll:  os.MkdirAll,
		writeFile: shared.AtomicWriteFile,
	}
}

func (installer *dockerInstaller) install() error {
	log.Info("[Docker 安装/发行版识别] 读取 ", installer.osReleasePath)
	distribution, err := installer.detectDistribution()
	if err != nil {
		return fmt.Errorf("Docker 安装在发行版识别阶段失败: %w", err)
	}
	fmt.Printf("识别结果：%s %s（%s）\n", distribution.Name, distribution.Version, distribution.Family)
	if _, err := installer.packageManager(distribution); err != nil {
		return fmt.Errorf("Docker 安装在发行版识别阶段失败: %w", err)
	}

	conflicts, err := installer.installedConflicts(distribution)
	if err != nil {
		return fmt.Errorf("Docker 安装在冲突包检测阶段失败: %w", err)
	}
	if len(conflicts) > 0 {
		confirmed, err := installer.confirm(conflicts)
		if err != nil {
			return fmt.Errorf("Docker 安装在冲突包确认阶段失败: %w", err)
		}
		if !confirmed {
			log.Info("已取消 Docker 安装，未修改系统")
			return nil
		}
	} else {
		log.Info("[Docker 安装/冲突包处理] 未检测到冲突包")
	}

	if !installer.commandExists("gpg") {
		return fmt.Errorf("Docker 安装在密钥验证阶段失败: 未找到 gpg，请先通过系统软件源安装 GnuPG")
	}
	keyURL, fingerprint := dockerKeyTrust(distribution)
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
		return fmt.Errorf("Docker 安装在密钥验证阶段失败: 指纹不匹配，已拒绝配置仓库")
	}
	log.Info("[Docker 安装/密钥验证] 指纹匹配")

	keyData, err := installer.readFile(tempPath)
	if err != nil {
		return fmt.Errorf("Docker 安装在仓库配置阶段失败: %w", err)
	}
	log.Info("[Docker 安装/仓库配置] 写入 Docker stable 签名仓库")
	if err := installer.configureRepository(distribution, keyData); err != nil {
		return fmt.Errorf("Docker 安装在仓库配置阶段失败: %w", err)
	}

	log.Info("[Docker 安装/元数据刷新] 刷新软件包元数据")
	if err := installer.refreshMetadata(distribution); err != nil {
		return fmt.Errorf("Docker 安装在元数据刷新阶段失败: %w", err)
	}
	if len(conflicts) > 0 {
		log.Info("[Docker 安装/冲突包处理] 卸载已确认的冲突包：", strings.Join(conflicts, ", "))
		if err := installer.removeConflicts(distribution, conflicts); err != nil {
			return fmt.Errorf("Docker 安装在冲突包卸载阶段失败: %w", err)
		}
	}

	log.Info("[Docker 安装/软件包安装] 安装：", strings.Join(dockerPackages, ", "))
	if err := installer.installPackages(distribution); err != nil {
		if len(conflicts) > 0 {
			return fmt.Errorf("Docker 安装在软件包安装阶段失败: %w；已卸载的冲突包不会自动恢复，请使用系统软件源重新安装：%s", err, strings.Join(conflicts, " "))
		}
		return fmt.Errorf("Docker 安装在软件包安装阶段失败: %w", err)
	}

	log.Info("[Docker 安装/服务启动] systemctl enable --now docker")
	if err := installer.run("systemctl", "enable", "--now", "docker"); err != nil {
		return fmt.Errorf("Docker 安装在服务启动阶段失败: %w", err)
	}
	log.Info("[Docker 安装/版本验证] docker version")
	version, err := installer.output("docker", "version")
	if err != nil {
		return fmt.Errorf("Docker 安装在版本验证阶段失败: %w（输出：%s）", err, strings.TrimSpace(version))
	}
	fmt.Println(version)
	log.Info("Docker 安装完成")
	return nil
}

func (installer *dockerInstaller) detectDistribution() (dockerDistribution, error) {
	data, err := installer.readFile(installer.osReleasePath)
	if err != nil {
		return dockerDistribution{}, err
	}
	values := parseOSRelease(string(data))
	distribution := dockerDistribution{
		ID: strings.ToLower(values["ID"]), Name: values["PRETTY_NAME"],
		Version: values["VERSION_ID"], Codename: values["VERSION_CODENAME"],
	}
	if distribution.Name == "" {
		distribution.Name = distribution.ID
	}
	switch distribution.ID {
	case "debian", "ubuntu":
		distribution.Family = "apt"
		if distribution.Codename == "" {
			return dockerDistribution{}, fmt.Errorf("%s 缺少 VERSION_CODENAME，无法生成 Docker 仓库", distribution.ID)
		}
	case "fedora", "centos", "rhel":
		distribution.Family = "rpm"
	default:
		return dockerDistribution{}, fmt.Errorf("不支持的 Linux 发行版 %q，仅支持 Debian、Ubuntu、Fedora、CentOS 和 RHEL", distribution.ID)
	}
	return distribution, nil
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
		if !ok {
			continue
		}
		values[key] = strings.Trim(strings.TrimSpace(value), "\"'")
	}
	return values
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
		source := fmt.Sprintf("Types: deb\nURIs: https://download.docker.com/linux/%s\nSuites: %s\nComponents: stable\nArchitectures: %s\nSigned-By: %s\n", distribution.ID, distribution.Codename, strings.TrimSpace(arch), keyPath)
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

func (installer *dockerInstaller) installedConflicts(distribution dockerDistribution) ([]string, error) {
	var candidates []string
	if distribution.Family == "apt" {
		if !installer.commandExists("dpkg-query") {
			return nil, fmt.Errorf("未找到 dpkg-query，无法安全检测冲突包")
		}
		candidates = []string{"docker.io", "docker-compose", "docker-doc", "podman-docker", "containerd", "runc"}
	} else {
		if !installer.commandExists("rpm") {
			return nil, fmt.Errorf("未找到 rpm，无法安全检测冲突包")
		}
		candidates = []string{"docker", "docker-client", "docker-client-latest", "docker-common", "docker-latest", "docker-latest-logrotate", "docker-logrotate", "docker-engine", "podman", "runc"}
	}
	var installed []string
	for _, name := range candidates {
		var output string
		var err error
		if distribution.Family == "apt" {
			output, err = installer.output("dpkg-query", "-W", "-f=${binary:Package}", name)
		} else {
			output, err = installer.output("rpm", "-q", "--qf", "%{NAME}", name)
		}
		if err == nil && strings.TrimSpace(output) != "" {
			installed = append(installed, strings.TrimSpace(output))
		}
	}
	sort.Strings(installed)
	return installed, nil
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

func downloadDockerKey(url, path string) error {
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
	_, copyErr := io.Copy(file, io.LimitReader(response.Body, 1024*1024+1))
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
	if info.Size() == 0 || info.Size() > 1024*1024 {
		return fmt.Errorf("下载的密钥大小异常: %d 字节", info.Size())
	}
	return nil
}
