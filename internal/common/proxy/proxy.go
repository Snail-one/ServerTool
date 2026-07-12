package proxy

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

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
	proxyWithAuth     = regexp.MustCompile(`^([^:]+):([^@]+)@([^:]+):([0-9]+)$`)
	proxyPlain        = regexp.MustCompile(`^([^:]+):([0-9]+)$`)
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

	fmt.Println("\033[32m[INFO]\033[0m 配置代理环境变量")
	fmt.Println()
	if currentProxy, ok := CurrentProxyURL(account); ok {
		fmt.Printf("当前代理服务器：%s\n", maskProxyURL(currentProxy))
		fmt.Println()
	}

	bashrc := filepath.Join(account.Home, ".bashrc")
	replaceUnmanagedProxy, err := confirmUnmanagedProxyOverride(view, bashrc)
	if err != nil {
		return err
	}
	if !replaceUnmanagedProxy {
		return nil
	}

	fmt.Println("\033[32m[INFO]\033[0m ip:port 格式，或 username:password@ip:port 格式")
	fmt.Println()

	raw, err := view.Ask("请输入代理地址: ")
	if err != nil {
		return err
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
	fmt.Println("代理配置完成")
	fmt.Println()
	fmt.Printf("代理地址：%s\n", maskProxyURL(proxyURL))
	fmt.Println()
	fmt.Println("立即生效请执行：")
	fmt.Println("source ~/.bashrc")
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
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("错误：代理地址不能为空")
	}

	port := ""
	if match := proxyWithAuth.FindStringSubmatch(raw); match != nil {
		port = match[4]
	} else if match := proxyPlain.FindStringSubmatch(raw); match != nil {
		port = match[2]
	} else {
		return "", fmt.Errorf("错误：代理格式不正确\n\n支持格式：\n127.0.0.1:8888\n192.168.1.1:8888\nadmin:123456@192.168.1.1:8888")
	}

	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return "", fmt.Errorf("错误：代理端口范围必须是 1-65535")
	}

	return "http://" + raw, nil
}

func writeProxyBlock(path, proxyURL string) error {
	return writeProxyBlockWithOptions(path, proxyURL, false)
}

func writeProxyBlockReplacingUnmanaged(path, proxyURL string) error {
	return writeProxyBlockWithOptions(path, proxyURL, true)
}

func writeProxyBlockWithOptions(path, proxyURL string, removeUnmanaged bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	content := shared.RemoveManagedBlock(string(data), proxyBegin, proxyEnd)
	if removeUnmanaged {
		content = removeUnmanagedProxyLines(content)
	}
	body := fmt.Sprintf(`export http_proxy="%s"
export https_proxy="%s"

export HTTP_PROXY="%s"
export HTTPS_PROXY="%s"

export no_proxy="%s"
export NO_PROXY="%s"
`, proxyURL, proxyURL, proxyURL, proxyURL, noProxy, noProxy)
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
	fmt.Println("1) 保留现有代理配置并返回")
	fmt.Println("2) 删除这些代理行，并写入新的代理配置")
	fmt.Println("0/q) 取消")
	fmt.Println()

	choice, err := view.Ask("输入选项: ")
	if err != nil {
		return false, err
	}

	switch strings.ToLower(choice) {
	case "2":
		return true, nil
	case "1", "", "0", "q", "exit":
		fmt.Println("已保留现有代理配置")
		return false, nil
	default:
		fmt.Println("无效选项，已取消代理配置")
		return false, nil
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

	content := shared.RemoveManagedBlock(string(data), proxyBegin, proxyEnd)
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
		if proxyShellEnvLine.MatchString(trimmed) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "")
}

func maskProxyURL(proxyURL string) string {
	prefix := "http://"
	raw := strings.TrimPrefix(proxyURL, prefix)

	match := proxyWithAuth.FindStringSubmatch(raw)
	if match == nil {
		return proxyURL
	}
	return fmt.Sprintf("%s%s:******@%s:%s", prefix, match[1], match[3], match[4])
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
