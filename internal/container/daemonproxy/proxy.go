package daemonproxy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	commonproxy "snail_tool/internal/common/proxy"
	"snail_tool/internal/log"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

const (
	dockerProxyDir  = "/etc/systemd/system/docker.service.d"
	dockerProxyPath = "/etc/systemd/system/docker.service.d/http-proxy.conf"
	dockerNoProxy   = "localhost,127.0.0.1,127.0.0.0/8,::1,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,169.254.0.0/16,*.local"
)

func Run(view *ui.UI) error {
	return ConfigureDockerDaemonProxy(view)
}

func ConfigureDockerDaemonProxy(view *ui.UI) error {
	if !system.IsRoot() {
		return fmt.Errorf("配置 Docker daemon 代理需要 root 权限，请使用 sudo 运行本工具")
	}
	if !system.SystemdUnitExists("docker.service") {
		return fmt.Errorf("未检测到 docker.service，无法配置 Docker daemon 代理")
	}

	fmt.Println("\033[32m[INFO]\033[0m 配置 Docker daemon 代理")
	fmt.Println()
	if currentContent := dockerProxyConfigContent(dockerProxyPath); strings.TrimSpace(currentContent) != "" {
		fmt.Printf("当前配置文件：%s\n", dockerProxyPath)
		fmt.Println("当前配置内容：")
		fmt.Print(currentContent)
		if !strings.HasSuffix(currentContent, "\n") {
			fmt.Println()
		}
		fmt.Println()
	}
	fmt.Println("支持 ip:port 格式，或 username:password@ip:port 格式")
	fmt.Println()

	raw, err := view.Ask("请输入 Docker 代理地址: ")
	if err != nil {
		return err
	}

	proxyURL, err := commonproxy.NormalizeProxy(raw)
	if err != nil {
		return err
	}
	proxyURL = dockerProxyURL(proxyURL)

	if err := writeDockerProxyConfig(dockerProxyPath, proxyURL); err != nil {
		return err
	}

	log.Info("重新加载 systemd 配置...")
	if err := system.Run("systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload 失败: %w", err)
	}

	log.Info("重启 Docker 服务...")
	if err := system.Run("systemctl", "restart", "docker"); err != nil {
		return fmt.Errorf("重启 Docker 服务失败: %w", err)
	}

	fmt.Println()
	fmt.Println("Docker daemon 代理配置完成")
	fmt.Printf("配置文件：%s\n", dockerProxyPath)
	fmt.Printf("写入地址：%s\n", dockerProxyDir)
	fmt.Printf("代理地址：%s\n", commonproxy.MaskProxyURL(proxyURL))
	fmt.Println("已执行：systemctl daemon-reload")
	fmt.Println("已执行：systemctl restart docker")
	return nil
}

func writeDockerProxyConfig(path, proxyURL string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(buildDockerProxyConfig(proxyURL)), 0644); err != nil {
		return err
	}
	return os.Chmod(path, 0644)
}

func buildDockerProxyConfig(proxyURL string) string {
	return fmt.Sprintf(`[Service]
Environment="HTTP_PROXY=%s"
Environment="HTTPS_PROXY=%s"
Environment="NO_PROXY=%s"
`, proxyURL, proxyURL, dockerNoProxy)
}

func configuredDockerProxyURL(path string) (string, bool) {
	content := dockerProxyConfigContent(path)
	return dockerProxyURLFromContent(content)
}

func dockerProxyURLFromContent(content string) (string, bool) {
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY"} {
		if value, ok := dockerEnvironmentValue(content, name); ok {
			return value, true
		}
	}
	return "", false
}

func dockerEnvironmentValue(content, name string) (string, bool) {
	prefix := `Environment="` + name + `=`
	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(strings.TrimRight(rawLine, "\r"))
		if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, `"`) {
			continue
		}
		value := strings.TrimSuffix(strings.TrimPrefix(line, prefix), `"`)
		if strings.TrimSpace(value) != "" {
			return value, true
		}
	}
	return "", false
}

func dockerProxyConfigContent(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func dockerProxyURL(proxyURL string) string {
	proxyURL = strings.TrimSpace(proxyURL)
	if strings.HasSuffix(proxyURL, "/") {
		return proxyURL
	}
	return proxyURL + "/"
}
