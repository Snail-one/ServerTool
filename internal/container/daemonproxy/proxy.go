package daemonproxy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	commonproxy "snail_tool/internal/common/proxy"
	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

const dockerProxyPath = "/etc/systemd/system/docker.service.d/http-proxy.conf"

var managedStore = shared.BackupStore{Root: shared.DataDir, Module: "docker"}

func Run(view *ui.UI) error {
	return ConfigureDockerDaemonProxy(view)
}

func ConfigureDockerDaemonProxy(view *ui.UI) error {
	ui.ClearScreen()
	ui.MenuTitle("容器管理", "Docker 配置", "配置服务代理")
	if !system.IsRoot() {
		return fmt.Errorf("配置 Docker 服务代理需要 root 权限，请使用 sudo 运行本工具")
	}
	if !system.SystemdUnitExists("docker.service") {
		return fmt.Errorf("未检测到 docker.service，无法配置 Docker 服务代理")
	}

	content, _, err := shared.ReadRegularFile(dockerProxyPath)
	if err != nil {
		return err
	}
	fields := append(proxyConfigFields(dockerProxyPath, string(content)),
		ui.CardField{Label: "应用方式", Value: "保存后重新加载 systemd 并重启 Docker"},
	)
	ui.PrintSectionCard("当前配置", fields...)
	fmt.Println()
	fmt.Println("支持 IP/主机名:端口或 [IPv6]:端口，可带 http:// 或 https://。")
	fmt.Println("认证格式：用户名:密码@地址；密码中的 @、#、% 等字符请使用 URL 编码。")
	ui.MenuExit("0/q", "返回")
	fmt.Println()

	raw, err := view.Ask("请输入 Docker 代理地址：")
	if err != nil {
		return err
	}

	if shared.IsReturnChoice(raw) {
		return shared.ErrReturnToMenu
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
	ui.PrintSuccessCard("Docker 服务代理配置完成",
		ui.CardField{Label: "配置文件", Value: dockerProxyPath},
		ui.CardField{Label: "代理地址", Value: commonproxy.MaskProxyURL(proxyURL)},
		ui.CardField{Label: "应用结果", Value: "systemd 已重新加载，Docker 已重启"},
	)
	return nil
}

func proxyConfigFields(path, content string) []ui.CardField {
	fields := []ui.CardField{{Label: "配置文件", Value: path}}
	httpProxy, httpOK := dockerEnvironmentValue(content, "HTTP_PROXY")
	httpsProxy, httpsOK := dockerEnvironmentValue(content, "HTTPS_PROXY")
	if httpOK && httpsOK && httpProxy == httpsProxy {
		fields = append(fields, ui.CardField{Label: "HTTP/HTTPS 代理", Value: commonproxy.MaskProxyURL(httpProxy)})
	} else {
		if httpOK {
			fields = append(fields, ui.CardField{Label: "HTTP 代理", Value: commonproxy.MaskProxyURL(httpProxy)})
		}
		if httpsOK {
			fields = append(fields, ui.CardField{Label: "HTTPS 代理", Value: commonproxy.MaskProxyURL(httpsProxy)})
		}
	}
	if noProxy, ok := dockerEnvironmentValue(content, "NO_PROXY"); ok {
		fields = append(fields, ui.CardField{Label: "绕过代理", Value: noProxy})
	}
	if len(fields) == 1 {
		status := "未识别代理设置"
		if strings.TrimSpace(content) == "" {
			status = "未配置"
		}
		fields = append(fields, ui.CardField{Label: "配置状态", Value: status})
	}
	return fields
}

func writeDockerProxyConfig(path, proxyURL string) error {
	proxyURL, err := commonproxy.NormalizeProxy(proxyURL)
	if err != nil {
		return err
	}
	proxyURL = dockerProxyURL(proxyURL)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data := []byte(buildDockerProxyConfig(proxyURL))
	if err := managedStore.RememberManaged(path, data); err != nil {
		return fmt.Errorf("记录 Docker 代理配置归属失败，未写入配置: %w", err)
	}
	return shared.AtomicWriteFile(path, data, shared.AtomicWriteOptions{Mode: 0644, ForceMode: true})
}

func buildDockerProxyConfig(proxyURL string) string {
	// Environment= expands systemd specifiers, including in URL-encoded
	// credentials. Double percent signs to preserve literal URL escapes.
	proxyURL = strings.ReplaceAll(proxyURL, "%", "%%")
	return fmt.Sprintf(`[Service]
Environment="HTTP_PROXY=%s"
Environment="HTTPS_PROXY=%s"
Environment="NO_PROXY=%s"
`, proxyURL, proxyURL, commonproxy.DefaultNoProxy())
}

// CurrentSummary 返回当前 Docker 服务代理的单行状态，供容器管理信息卡显示。
func CurrentSummary() string {
	return summarizeProxyConfig(dockerProxyPath)
}

func summarizeProxyConfig(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "未配置"
		}
		return "无法读取"
	}
	content := string(data)
	if strings.TrimSpace(content) == "" {
		return "未配置"
	}
	httpProxy, httpOK := dockerEnvironmentValue(content, "HTTP_PROXY")
	httpsProxy, httpsOK := dockerEnvironmentValue(content, "HTTPS_PROXY")
	switch {
	case httpOK && httpsOK && httpProxy != httpsProxy:
		return "HTTP " + commonproxy.MaskProxyURL(httpProxy) + "，HTTPS " + commonproxy.MaskProxyURL(httpsProxy)
	case httpOK:
		return commonproxy.MaskProxyURL(httpProxy)
	case httpsOK:
		return commonproxy.MaskProxyURL(httpsProxy)
	default:
		return "未识别代理地址"
	}
}

func dockerEnvironmentValue(content, name string) (string, bool) {
	prefix := `Environment="` + name + `=`
	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(strings.TrimRight(rawLine, "\r"))
		if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, `"`) {
			continue
		}
		value := strings.TrimSuffix(strings.TrimPrefix(line, prefix), `"`)
		value = strings.ReplaceAll(value, "%%", "%")
		if strings.TrimSpace(value) != "" {
			return value, true
		}
	}
	return "", false
}

func dockerProxyURL(proxyURL string) string {
	proxyURL = strings.TrimSpace(proxyURL)
	if strings.HasSuffix(proxyURL, "/") {
		return proxyURL
	}
	return proxyURL + "/"
}
