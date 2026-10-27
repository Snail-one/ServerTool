package proxy

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"snail_tool/internal/input"
	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

const (
	proxyBegin = "# ===== BEGIN SNAIL PROXY CONFIG ====="
	proxyEnd   = "# ===== END SNAIL PROXY CONFIG ====="
	noProxy    = "localhost,127.0.0.1,::1,.localhost,.local,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,169.254.0.0/16,fc00::/7,fe80::/10"
)

var (
	proxyShellEnvLine = regexp.MustCompile(`^[ \t]*(?:export[ \t]+)?(http_proxy|https_proxy|HTTP_PROXY|HTTPS_PROXY|no_proxy|NO_PROXY)=(?:"([^"]*)"|'([^']*)'|([^ \t\r\n;]*))`)
	proxyEnvNames     = []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"}
)

type proxyAssignment struct {
	name  string
	value string
}

func ProxyMarkers() (string, string) {
	return proxyBegin, proxyEnd
}

func ProxyEnvNames() []string {
	return append([]string{}, proxyEnvNames...)
}

func DefaultNoProxy() string {
	return noProxy
}

func ProxyCleanupEnvNames() []string {
	names := ProxyEnvNames()
	return append(names, "NO_PROXY", "no_proxy")
}

func Run(view *ui.UI) error {
	return ConfigureProxy(view)
}

func ConfigureProxy(view *ui.UI) error {
	account, err := system.CurrentTargetUser()
	if err != nil {
		return err
	}

	log.Info("配置代理环境变量")
	fmt.Println()
	if currentProxy, ok := CurrentProxyURL(account); ok {
		ui.PrintInfoCard("当前代理配置", ui.CardField{Label: "代理服务器", Value: maskProxyURL(currentProxy)})
		fmt.Println()
	}

	bashrc := filepath.Join(account.Home, ".bashrc")
	replaceUnmanagedProxy, err := confirmUnmanagedProxyOverride(view, bashrc)
	if err != nil {
		return err
	}
	if !replaceUnmanagedProxy {
		return shared.ErrReturnToMenu
	}

	log.Info("支持 IP/主机名:端口或 [IPv6]:端口，可带 http:// 或 https://")
	log.Info("认证格式：用户名:密码@地址；密码中的 @、#、% 等字符请使用 URL 编码")
	fmt.Println()

	raw, err := view.Ask("请输入代理地址：")
	if err != nil {
		return err
	}

	if shared.IsReturnChoice(raw) {
		return shared.ErrReturnToMenu
	}
	proxyURL, err := normalizeProxy(raw)
	if err != nil {
		return err
	}

	if err := shared.EnsureFileWithOptions(bashrc, shared.AtomicWriteOptions{
		Mode: 0644, Owner: &shared.FileOwner{UID: account.UID, GID: account.GID},
	}); err != nil {
		return err
	}
	if err := writeProxyBlockReplacingUnmanaged(bashrc, proxyURL); err != nil {
		return err
	}
	if err := system.ChownPath(bashrc, account, false); err != nil {
		return err
	}
	if err := os.Chmod(bashrc, 0644); err != nil {
		return err
	}

	fmt.Println()
	ui.PrintSuccessCard("代理配置完成",
		ui.CardField{Label: "配置文件", Value: bashrc},
		ui.CardField{Label: "代理地址", Value: maskProxyURL(proxyURL)},
		ui.CardField{Label: "立即生效", Value: "source " + bashrc},
	)
	return nil
}

func CurrentProxyURL(account *system.Account) (string, bool) {
	if proxyURL, ok := currentProxyURLFromInvokerEnv(); ok {
		return proxyURL, true
	}

	if proxyURL, ok := currentProxyURLFromEnv(); ok {
		return proxyURL, true
	}

	return ConfiguredProxyURL(account)
}

func ConfiguredProxyURL(account *system.Account) (string, bool) {
	bashrc := filepath.Join(account.Home, ".bashrc")
	return proxyURLFromShellContent(shared.ReadFileString(bashrc))
}

func IsProxyConfigured(account *system.Account) bool {
	_, ok := ConfiguredProxyURL(account)
	return ok
}

func NormalizeProxy(raw string) (string, error) {
	return normalizeProxy(raw)
}

func MaskProxyURL(proxyURL string) string {
	return maskProxyURL(proxyURL)
}

func currentProxyURLFromInvokerEnv() (string, bool) {
	if strings.TrimSpace(os.Getenv("SUDO_USER")) == "" {
		return "", false
	}

	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(os.Getppid()), "environ"))
	if err != nil {
		return "", false
	}
	return proxyURLFromEnvPairs(strings.Split(string(data), "\x00"))
}

func currentProxyURLFromEnv() (string, bool) {
	return proxyURLFromEnvPairs(os.Environ())
}

func proxyURLFromEnvPairs(pairs []string) (string, bool) {
	for _, name := range proxyEnvNames {
		prefix := name + "="
		for _, pair := range pairs {
			if strings.HasPrefix(pair, prefix) {
				value := strings.TrimSpace(strings.TrimPrefix(pair, prefix))
				if value != "" {
					return value, true
				}
			}
		}
	}
	return "", false
}

func proxyURLFromShellContent(content string) (string, bool) {
	assignments := proxyAssignmentsFromContent(content)
	for _, name := range proxyEnvNames {
		for _, assignment := range assignments {
			if assignment.name != name {
				continue
			}
			value := strings.TrimSpace(assignment.value)
			if value != "" {
				return value, true
			}
		}
	}
	return "", false
}

func normalizeProxy(raw string) (string, error) {
	if err := input.Text(raw); err != nil {
		return "", err
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("错误：代理地址不能为空")
	}
	if strings.ContainsFunc(raw, unicode.IsSpace) {
		return "", fmt.Errorf("错误：代理地址不能包含空白字符；认证信息中的空格请使用 %%20")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Opaque != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("错误：请输入有效的 HTTP/HTTPS 代理地址，例如 192.168.1.1:7890")
	}
	if (parsed.EscapedPath() != "" && parsed.EscapedPath() != "/") || parsed.RawQuery != "" || parsed.ForceQuery || strings.Contains(raw, "#") {
		return "", fmt.Errorf("错误：代理地址不能包含路径、查询参数或片段；认证信息中的特殊字符请使用 URL 编码")
	}
	host, port, err := net.SplitHostPort(parsed.Host)
	if err != nil || !validProxyHost(host) {
		return "", fmt.Errorf("错误：代理主机或端口格式无效；支持 IP、主机名和 [IPv6]:端口")
	}
	number, err := input.Number(port, 1, 65535)
	if err != nil {
		return "", fmt.Errorf("错误：代理端口范围必须是 1-65535")
	}
	if parsed.User != nil {
		password, hasPassword := parsed.User.Password()
		if parsed.User.Username() == "" || !hasPassword || password == "" {
			return "", fmt.Errorf("错误：代理认证必须同时提供非空用户名和密码")
		}
		if input.Text(parsed.User.Username()) != nil || input.Text(password) != nil || strings.ContainsFunc(parsed.User.Username()+password, unicode.IsControl) {
			return "", fmt.Errorf("错误：代理认证包含乱码、控制字符或不可见字符")
		}
		parsed.User = url.UserPassword(parsed.User.Username(), password)
	}
	parsed.Host = net.JoinHostPort(strings.ToLower(host), strconv.Itoa(number))
	parsed.Path = ""
	return parsed.String(), nil
}

func validProxyHost(host string) bool {
	if addr, err := netip.ParseAddr(host); err == nil {
		return addr.Zone() == ""
	}
	name := strings.TrimSuffix(host, ".")
	if name == "" || len(name) > 253 || strings.Trim(name, "0123456789.") == "" {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-') {
				return false
			}
		}
	}
	return true
}

func writeProxyBlockReplacingUnmanaged(path, proxyURL string) error {
	proxyURL, err := normalizeProxy(proxyURL)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	content, err := shared.RemoveManagedBlockChecked(string(data), proxyBegin, proxyEnd)
	if err != nil {
		return err
	}
	content = removeUnmanagedProxyLines(content)
	body := fmt.Sprintf(`export http_proxy="%s"
export https_proxy="%s"

export HTTP_PROXY="%s"
export HTTPS_PROXY="%s"

export no_proxy="%s"
export NO_PROXY="%s"
`, proxyURL, proxyURL, proxyURL, proxyURL, noProxy, noProxy)
	// A URL may contain literal shell characters in its credentials. Escape them
	// for double-quoted assignments so sourcing .bashrc preserves the exact value.
	body = strings.NewReplacer("\\", "\\\\", "`", "\\`", "$", "\\$").Replace(body)
	block := shared.FormatManagedBlock(proxyBegin, body, proxyEnd)

	return shared.AtomicWriteFile(path, []byte(shared.AppendBlock(content, block)), shared.AtomicWriteOptions{Mode: 0644})
}

func confirmUnmanagedProxyOverride(view *ui.UI, bashrc string) (bool, error) {
	assignments, err := unmanagedProxyAssignments(bashrc)
	if err != nil {
		return false, err
	}
	if len(assignments) == 0 {
		return true, nil
	}

	fmt.Printf("检测到 %s 中已有非本工具管理的代理配置：\n", bashrc)
	for _, assignment := range assignments {
		fmt.Printf("- %s=%s\n", assignment.name, maskProxyValue(assignment.value))
	}
	fmt.Println()
	ui.MenuOptionHint("1", "保留现有配置", "不修改并返回")
	ui.MenuOptionHint("2", "替换现有配置", "删除已有代理行并写入新配置")
	ui.MenuExit("0/q", "取消")
	fmt.Println()

	for {
		choice, err := view.Ask("请选择：")
		if err != nil {
			return false, err
		}
		if shared.IsReturnChoice(choice) {
			fmt.Println("已保留现有代理配置")
			return false, nil
		}

		switch strings.ToLower(choice) {
		case "2":
			return true, nil
		case "1":
			fmt.Println("已保留现有代理配置")
			return false, nil
		default:
			ui.InvalidChoice()
		}
	}
}

func unmanagedProxyAssignments(path string) ([]proxyAssignment, error) {
	if !system.FileExists(path) {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	content, err := shared.RemoveManagedBlockChecked(string(data), proxyBegin, proxyEnd)
	if err != nil {
		return nil, err
	}
	return proxyAssignmentsFromContent(content), nil
}

func proxyAssignmentsFromContent(content string) []proxyAssignment {
	assignments := make([]proxyAssignment, 0)
	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimRight(rawLine, "\r")
		match := proxyShellEnvLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		assignments = append(assignments, proxyAssignment{
			name:  match[1],
			value: firstNonEmpty(match[2:]...),
		})
		if match[2] != "" {
			assignments[len(assignments)-1].value = strings.NewReplacer(`\\`, `\`, `\$`, `$`, "\\`", "`").Replace(match[2])
		}
	}
	return assignments
}

func removeUnmanagedProxyLines(content string) string {
	lines := strings.SplitAfter(content, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		trimmed := strings.TrimRight(line, "\r\n")
		match := proxyShellEnvLine.FindStringIndex(trimmed)
		if match != nil && strings.TrimSpace(trimmed[match[1]:]) == "" {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "")
}

func maskProxyURL(proxyURL string) string {
	return maskProxyValue(proxyURL)
}

func maskProxyValue(value string) string {
	value = strings.TrimSpace(value)
	if !strings.Contains(value, "@") {
		return value
	}

	at := strings.LastIndex(value, "@")
	schemeEnd := strings.Index(value, "://")
	userInfoStart := 0
	if schemeEnd >= 0 {
		userInfoStart = schemeEnd + len("://")
	}
	if at <= userInfoStart {
		return value
	}

	userInfo := value[userInfoStart:at]
	user := userInfo
	if colon := strings.Index(userInfo, ":"); colon >= 0 {
		user = userInfo[:colon]
	}
	return value[:userInfoStart] + user + ":******@" + value[at+1:]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
