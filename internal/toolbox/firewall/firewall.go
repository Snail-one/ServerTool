package firewall

import (
	_ "embed"
	"fmt"
	"os"
	"strconv"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

//go:embed nftables.conf
var configTemplate string

var (
	configPath    = "/etc/nftables.conf"
	commandExists = system.CommandExists
	commandRun    = system.Run
	commandOutput = system.Output
	isRoot        = system.IsRoot
)

func Run(view *ui.UI) error {
	ui.MenuTitle("防火墙管理")
	if !isRoot() {
		return fmt.Errorf("防火墙配置需要 root 权限，请使用 sudo 运行本工具")
	}
	if !commandExists("nft") {
		confirmed, err := view.Confirm("未检测到 nft，是否安装 nftables？(y/N)：")
		if err != nil {
			return err
		}
		if !confirmed {
			log.Info("已取消防火墙配置")
			return nil
		}
		if err := installNftables(); err != nil {
			return err
		}
		if !commandExists("nft") {
			return fmt.Errorf("安装后仍未检测到 nft，请检查 nftables 安装情况")
		}
	}
	ports, err := detectSSHPorts()
	if err != nil {
		return err
	}
	ui.PrintInfoCard("检测到 SSH 监听端口",
		ui.CardField{Label: "TCP 端口", Value: portList(ports)},
		ui.CardField{Label: "开放范围", Value: "公网、内网及 IPv4 / IPv6"},
	)
	privateWeb, err := view.Confirm("是否默认开放内网 TCP 80、443 端口（IPv4 / IPv6）？(y/N)：")
	if err != nil {
		return err
	}
	content, err := buildConfig(ports, privateWeb)
	if err != nil {
		return err
	}
	fmt.Println(content)
	confirmed, err := view.Confirm("是否校验并写入以上配置到 /etc/nftables.conf（已有文件会先备份）？(y/N)：")
	if err != nil {
		return err
	}
	if !confirmed {
		log.Info("已取消配置写入")
		return nil
	}
	// Detect again after the interaction so a changed SSH listener cannot be
	// silently omitted from the generated firewall.
	latest, err := detectSSHPorts()
	if err != nil {
		return err
	}
	if portList(latest) != portList(ports) {
		return fmt.Errorf("SSH 监听端口已变化，请重新进入防火墙管理生成配置")
	}
	if err := writeConfig(content); err != nil {
		return err
	}
	ui.PrintSuccessCard("nftables 配置已写入",
		ui.CardField{Label: "配置文件", Value: configPath},
		ui.CardField{Label: "SSH 端口", Value: portList(ports)},
		ui.CardField{Label: "当前状态", Value: "配置已校验，等待手动加载"},
		ui.CardField{Label: "立即生效", Value: "sudo nft -f /etc/nftables.conf"},
	)
	return nil
}

func buildConfig(ports []int, privateWeb bool) (string, error) {
	if len(ports) == 0 {
		return "", fmt.Errorf("SSH 端口不能为空")
	}
	var definitions, references []string
	for i, port := range ports {
		if port < 1 || port > 65535 {
			return "", fmt.Errorf("SSH 端口无效: %d", port)
		}
		name := "snail_ssh_port"
		if i > 0 {
			name += "_" + strconv.Itoa(i+1)
		}
		definitions = append(definitions, fmt.Sprintf("define %s = %d", name, port))
		references = append(references, "$"+name)
	}
	sshPorts := strings.Join(references, ", ")
	privatePorts := sshPorts
	if privateWeb {
		privatePorts += ", 80, 443"
	}
	return strings.NewReplacer(
		"__SSH_DEFINITIONS__", strings.Join(definitions, "\n"),
		"__SSH_REFERENCES__", sshPorts,
		"__PRIVATE_PORTS__", privatePorts,
	).Replace(configTemplate), nil
}

func portList(ports []int) string {
	values := make([]string, len(ports))
	for i, port := range ports {
		values[i] = strconv.Itoa(port)
	}
	return strings.Join(values, ", ")
}

func writeConfig(content string) error {
	if info, err := os.Lstat(configPath); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("拒绝覆盖非普通文件或软链接: %s", configPath)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	tmp, err := os.CreateTemp("", "snail-nftables-*.conf")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if out, err := commandOutput("nft", "-c", "-f", tmp.Name()); err != nil {
		return fmt.Errorf("nftables 配置校验失败，未写入正式配置: %w: %s", err, strings.TrimSpace(out))
	}
	if _, err := os.Lstat(configPath); err == nil {
		backup, err := system.Backup(configPath)
		if err != nil {
			return err
		}
		log.Info("已备份原防火墙配置：", backup)
	} else if !os.IsNotExist(err) {
		return err
	}
	return shared.AtomicWriteFile(configPath, []byte(content), shared.AtomicWriteOptions{Mode: 0644})
}

func installNftables() error {
	for _, manager := range []struct {
		name string
		args []string
	}{
		{"apt-get", []string{"install", "-y", "nftables"}},
		{"apt", []string{"install", "-y", "nftables"}},
		{"dnf", []string{"install", "-y", "nftables"}},
		{"yum", []string{"install", "-y", "nftables"}},
		{"pacman", []string{"-S", "--noconfirm", "--needed", "nftables"}},
		{"zypper", []string{"--non-interactive", "install", "nftables"}},
		{"apk", []string{"add", "nftables"}},
	} {
		if !commandExists(manager.name) {
			continue
		}
		if manager.name == "apt" || manager.name == "apt-get" {
			if err := commandRun(manager.name, "update"); err != nil {
				return fmt.Errorf("更新软件包索引失败: %w", err)
			}
		}
		return commandRun(manager.name, manager.args...)
	}
	return fmt.Errorf("未识别支持的包管理器，请手动安装 nftables")
}
