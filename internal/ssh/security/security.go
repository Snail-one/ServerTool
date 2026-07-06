package security

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

type sshdSettings map[string][]string

type sshdSecurityRow struct {
	name      string
	value     string
	status    string
	directive string
}

type sshdPortDirective struct {
	path string
	line int
	text string
}

const (
	managedSSHDConfigBegin  = "# ===== BEGIN SNAIL TOOL SERVERTOOL SSHD CONFIG ====="
	managedSSHDConfigEnd    = "# ===== END SNAIL TOOL SERVERTOOL SSHD CONFIG ====="
	managedSSHDIncludeBegin = "# ===== BEGIN SNAIL SSH INCLUDE ====="
	managedSSHDIncludeEnd   = "# ===== END SNAIL SSH INCLUDE ====="
	sshdConfigPath          = "/etc/ssh/sshd_config"
	sshdConfigDir           = "/etc/ssh/sshd_config.d"
	customSSHDConfigPath    = "/etc/ssh/sshd_config.d/99-custom.conf"
	sshdCommandPath         = "/usr/sbin/sshd"
)

func ManagedSSHDIncludeMarkers() (string, string) {
	return managedSSHDIncludeBegin, managedSSHDIncludeEnd
}

func ManagedSSHDConfigMarkers() (string, string) {
	return managedSSHDConfigBegin, managedSSHDConfigEnd
}

func CustomSSHDConfigPath() string {
	return customSSHDConfigPath
}

func SSHDConfigPath() string {
	return sshdConfigPath
}

func Run(view *ui.UI) error {
	return ConfigureSSHSecurity(view)
}

func ConfigureSSHSecurity(view *ui.UI) error {
	account, err := system.CurrentTargetUser()
	if err != nil {
		return err
	}

	log.Info("当前配置用户：", account.Name)
	fmt.Println()
	return configureSSHDHardening(view, account)
}

func ShowStatus() error {
	settings, source, err := loadSSHDSettings()
	if err != nil {
		return err
	}

	fmt.Println("当前 SSH 安全配置：")
	fmt.Println()
	fmt.Println("配置来源：", source)
	fmt.Println()
	printSSHSecurityRows(settings)
	return nil
}

func IsConfigured() bool {
	return isManagedSSHDConfig(shared.ReadFileString(customSSHDConfigPath))
}

func IsManagedSSHDConfigContent(content string) bool {
	return isManagedSSHDConfig(content)
}

func ReloadService() error {
	return reloadSSHService()
}

func printSSHSecurityRows(settings sshdSettings) {
	for _, row := range buildSSHSecurityRows(settings) {
		if row.status == "" {
			fmt.Printf("- %s：%s\n", row.name, row.value)
		} else {
			fmt.Printf("- %s：%s [%s]\n", row.name, row.value, row.status)
		}
		if row.directive != "" {
			fmt.Printf("  配置项：%s\n", row.directive)
		}
	}
}

func configureSSHDHardening(view *ui.UI, account *system.Account) error {
	if account.Name != "root" && !system.UserInAdminGroup(account.Name) {
		log.Warn("用户 ", account.Name, " 不在 sudo/wheel 用户组中")
		confirmed, err := view.Confirm("继续配置可能导致无法提权，是否强行继续？(y/N): ")
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}

	authorizedKeysConfirmed, err := confirmAuthorizedKeysPresent(view, account)
	if err != nil {
		return err
	}
	if !authorizedKeysConfirmed {
		return nil
	}

	shouldWriteConfig, err := confirmSSHDConfigOverwrite(view)
	if err != nil {
		return err
	}
	if !shouldWriteConfig {
		fmt.Println()
		log.Info("已取消 SSH 服务安全配置写入")
		return nil
	}

	fmt.Println()
	rawPort, err := view.Ask("请输入 SSH 端口（直接回车随机生成）: ")
	if err != nil {
		return err
	}
	port, err := chooseSSHPort(rawPort)
	if err != nil {
		return err
	}

	permitRootLogin := "no"
	if account.Name == "root" {
		permitRootLogin = "prohibit-password"
		log.Warn("当前配置用户是 root：保留 root 公钥登录，不禁用 root 登录")
	}

	disableSSHDConfigPorts, err := confirmDisableSSHDConfigPorts(view, port)
	if err != nil {
		return err
	}

	if err := writeSSHDConfig(port, permitRootLogin, disableSSHDConfigPorts); err != nil {
		return err
	}
	if err := reloadSSHService(); err != nil {
		return err
	}

	settings, source, err := loadSSHDSettings()
	if err != nil {
		return err
	}
	effectivePort := firstSSHDSetting(settings, "Port")
	if effectivePort == "" {
		effectivePort = strconv.Itoa(port)
	}

	fmt.Println()
	log.Info("SSH 服务安全配置完成")
	fmt.Println()
	fmt.Printf("用户：%s\n", account.Name)
	fmt.Println()
	fmt.Println("本次写入 SSH 配置：")
	printSSHDConfig(customSSHDConfigPath, buildSSHDConfig(port, permitRootLogin))
	if disableSSHDConfigPorts {
		fmt.Println("旧端口处理：已按确认注释旧 Port 配置")
	} else {
		fmt.Println("旧端口处理：未注释旧 Port 配置")
	}
	fmt.Println()
	fmt.Println("生效配置来源：", source)
	fmt.Println()
	fmt.Println("生效 SSH 安全配置：")
	printSSHSecurityRows(settings)
	fmt.Println()
	fmt.Println("连接方式：")
	fmt.Printf("ssh -p %s %s@服务器IP\n", effectivePort, account.Name)
	fmt.Println()
	log.Warn("请先新开一个终端测试 SSH 登录成功后，再关闭当前会话。")
	return nil
}

func confirmAuthorizedKeysPresent(view *ui.UI, account *system.Account) (bool, error) {
	authKeys := filepath.Join(account.Home, ".ssh", "authorized_keys")
	if system.FileNonEmpty(authKeys) {
		return true, nil
	}

	log.Warn("未检测到当前用户 SSH 公钥：", authKeys)
	confirmed, err := view.Confirm("继续禁用密码登录可能导致无法 SSH 登录，是否强行继续？(y/N): ")
	if err != nil {
		return false, err
	}
	if !confirmed {
		fmt.Println("已取消 SSH 服务安全配置写入")
	}
	return confirmed, nil
}

func confirmSSHDConfigOverwrite(view *ui.UI) (bool, error) {
	if !system.FileExists(customSSHDConfigPath) {
		return true, nil
	}

	existing, err := os.ReadFile(customSSHDConfigPath)
	if err != nil {
		return false, err
	}
	existingContent := string(existing)
	if strings.TrimSpace(existingContent) == "" {
		return true, nil
	}
	if !isManagedSSHDConfig(existingContent) {
		printSSHDConfig(customSSHDConfigPath, existingContent)
		return view.Confirm("检测到该 SSH 配置文件不是本工具生成，继续会备份并覆盖，是否继续？(y/N): ")
	}

	printSSHDConfig(customSSHDConfigPath, existingContent)
	return view.Confirm("检测到该 SSH 配置文件由脚本创建，是否覆盖并重新生成？(y/N): ")
}

func confirmDisableSSHDConfigPorts(view *ui.UI, port int) (bool, error) {
	paths, err := sshdPortConfigPaths()
	if err != nil {
		return false, err
	}

	directives, err := activeSSHDPortDirectivesInFiles(paths)
	if err != nil {
		return false, err
	}
	if len(directives) == 0 {
		return false, nil
	}

	fmt.Println()
	fmt.Println("检测到已有 Port 配置：")
	for _, directive := range directives {
		fmt.Printf("- %s 第 %d 行：%s\n", directive.path, directive.line, strings.TrimSpace(directive.text))
	}
	fmt.Printf("新端口将写入 %s：Port %d\n", customSSHDConfigPath, port)
	return confirmDefaultYes(view, "是否注释这些 Port 配置，关闭旧端口？(Y/n): ")
}

func confirmDefaultYes(view *ui.UI, prompt string) (bool, error) {
	for {
		value, err := view.Ask(prompt)
		if err != nil {
			return false, err
		}

		switch strings.ToLower(strings.TrimSpace(value)) {
		case "", "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Println("请输入 y 或 n")
		}
	}
}

func chooseSSHPort(raw string) (int, error) {
	if raw == "" {
		log.Info("生成随机 SSH 端口...")
		return system.RandomFreePort(), nil
	}

	port, err := system.ValidatePort(raw)
	if err != nil {
		return 0, err
	}
	if system.PortInUse(port) {
		return 0, fmt.Errorf("端口 %d 已被占用", port)
	}
	return port, nil
}

func sshdPortConfigPaths() ([]string, error) {
	return sshdPortConfigPathsFor(sshdConfigPath, sshdConfigDir, customSSHDConfigPath)
}

func sshdPortConfigPathsFor(configPath, configDir, customPath string) ([]string, error) {
	paths := []string{configPath}

	matches, err := filepath.Glob(filepath.Join(configDir, "*.conf"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)

	cleanCustomPath := filepath.Clean(customPath)
	for _, path := range matches {
		if filepath.Clean(path) == cleanCustomPath {
			continue
		}

		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if info.IsDir() {
			continue
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func activeSSHDPortDirectivesInFiles(paths []string) ([]sshdPortDirective, error) {
	var directives []sshdPortDirective
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("读取 SSH 配置文件失败 %s: %w", path, err)
		}

		for _, directive := range activeSSHDPortDirectives(string(data)) {
			directive.path = path
			directives = append(directives, directive)
		}
	}
	return directives, nil
}

func activeSSHDPortDirectives(content string) []sshdPortDirective {
	var directives []sshdPortDirective
	for index, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimRight(rawLine, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		fields := strings.Fields(trimmed)
		if len(fields) >= 2 && strings.EqualFold(fields[0], "Port") {
			directives = append(directives, sshdPortDirective{
				line: index + 1,
				text: line,
			})
		}
	}
	return directives
}

func commentActiveSSHDPortDirectives(content string) (string, bool) {
	lines := strings.SplitAfter(content, "\n")
	var builder strings.Builder
	changed := false

	for _, line := range lines {
		body, ending := splitLineEnding(line)
		if isActiveSSHDPortDirective(body) {
			builder.WriteString("# SNAIL disabled duplicate Port: ")
			changed = true
		}
		builder.WriteString(body)
		builder.WriteString(ending)
	}

	return builder.String(), changed
}

func splitLineEnding(line string) (string, string) {
	if strings.HasSuffix(line, "\r\n") {
		return strings.TrimSuffix(line, "\r\n"), "\r\n"
	}
	if strings.HasSuffix(line, "\n") {
		return strings.TrimSuffix(line, "\n"), "\n"
	}
	return line, ""
}

func isActiveSSHDPortDirective(line string) bool {
	trimmed := strings.TrimSpace(strings.TrimRight(line, "\r"))
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return false
	}

	fields := strings.Fields(trimmed)
	return len(fields) >= 2 && strings.EqualFold(fields[0], "Port")
}

func writeSSHDConfig(port int, permitRootLogin string, disableSSHDConfigPorts bool) error {
	if err := os.MkdirAll(sshdConfigDir, 0755); err != nil {
		return err
	}

	content := buildSSHDConfig(port, permitRootLogin)
	fmt.Println()
	log.Info("验证新 SSH 配置...")
	if err := validateSSHDConfig(content); err != nil {
		return err
	}

	fmt.Println()
	log.Info("检查 Include 配置...")
	data, err := os.ReadFile(sshdConfigPath)
	if err != nil {
		return err
	}
	includeRe := regexp.MustCompile(`(?m)^[[:space:]]*Include[[:space:]]+/etc/ssh/sshd_config\.d/\*\.conf`)
	if !includeRe.Match(data) {
		file, err := os.OpenFile(sshdConfigPath, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		if !strings.HasSuffix(string(data), "\n") {
			if _, err := file.WriteString("\n"); err != nil {
				_ = file.Close()
				return err
			}
		}
		block := fmt.Sprintf("%s\nInclude /etc/ssh/sshd_config.d/*.conf\n%s\n", managedSSHDIncludeBegin, managedSSHDIncludeEnd)
		if _, err := file.WriteString(block); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		log.Info("已自动添加 Include 配置")
	}

	if system.FileExists(customSSHDConfigPath) {
		backup, err := system.Backup(customSSHDConfigPath)
		if err != nil {
			return err
		}
		log.Info("已备份原 SSH 自定义配置：", backup)
	}

	fmt.Println()
	log.Info("写入自定义 SSH 配置...")
	if err := os.WriteFile(customSSHDConfigPath, []byte(content), 0644); err != nil {
		return err
	}
	if err := os.Chmod(customSSHDConfigPath, 0644); err != nil {
		return err
	}
	printSSHDConfig(customSSHDConfigPath, content)

	if disableSSHDConfigPorts {
		if err := disableSSHDConfigPortsInFiles(); err != nil {
			return err
		}
	}

	fmt.Println()
	log.Info("验证当前 sshd 配置...")
	if err := system.Run(sshdCommandPath, "-t"); err != nil {
		return fmt.Errorf("sshd 配置校验失败: %w", err)
	}
	return nil
}

func disableSSHDConfigPortsInFiles() error {
	paths, err := sshdPortConfigPaths()
	if err != nil {
		return err
	}

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("读取 SSH 配置文件失败 %s: %w", path, err)
		}

		cleaned, changed := commentActiveSSHDPortDirectives(string(data))
		if !changed {
			continue
		}

		mode := os.FileMode(0644)
		if info, err := os.Stat(path); err == nil {
			mode = info.Mode().Perm()
		}
		backup, err := system.Backup(path)
		if err != nil {
			return err
		}
		log.Info("已备份 SSH 配置：", backup)
		if err := os.WriteFile(path, []byte(cleaned), mode); err != nil {
			return err
		}
		log.Info("已注释 ", path, " 中的 Port 配置")
	}
	return nil
}

func buildSSHDConfig(port int, permitRootLogin string) string {
	return fmt.Sprintf("%s\n\nPort %d\nPasswordAuthentication no\nPermitRootLogin %s\nPubkeyAuthentication yes\n%s\n",
		managedSSHDConfigBegin, port, permitRootLogin, managedSSHDConfigEnd)
}

func isManagedSSHDConfig(content string) bool {
	return strings.Contains(content, managedSSHDConfigBegin) &&
		strings.Contains(content, managedSSHDConfigEnd)
}

func loadSSHDSettings() (sshdSettings, string, error) {
	output, err := system.Output(sshdCommandPath, "-T")
	if err != nil {
		detail := strings.TrimSpace(output)
		if detail != "" {
			return nil, "", fmt.Errorf("读取当前 sshd 生效配置失败: %w: %s", err, detail)
		}
		return nil, "", fmt.Errorf("读取当前 sshd 生效配置失败: %w", err)
	}

	settings := parseSSHDSettings(output)
	if len(settings) == 0 {
		return nil, "", fmt.Errorf("读取当前 sshd 生效配置失败: sshd -T 未返回配置")
	}
	return settings, sshdCommandPath + " -T", nil
}

func parseSSHDSettings(output string) sshdSettings {
	settings := make(sshdSettings)
	for _, rawLine := range strings.Split(output, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		key := strings.ToLower(fields[0])
		value := strings.TrimSpace(strings.TrimPrefix(line, fields[0]))
		if value == "" {
			continue
		}
		settings[key] = append(settings[key], value)
	}
	return settings
}

func buildSSHSecurityRows(settings sshdSettings) []sshdSecurityRow {
	return []sshdSecurityRow{
		sshdPortRow(settings),
		sshdYesNoRow(settings, "PubkeyAuthentication", "密钥登录", "已开启", "已关闭", true),
		sshdValueRow(settings, "AuthorizedKeysFile", "密钥文件路径", "信息", "未检测到"),
		sshdYesNoRow(settings, "PasswordAuthentication", "密码登录", "已开启", "已禁用", false),
		sshdKeyboardInteractiveRow(settings),
		sshdPermitRootLoginRow(settings),
		sshdYesNoRow(settings, "PermitEmptyPasswords", "空密码登录", "已允许", "已禁止", false),
		sshdYesNoInfoRow(settings, "UsePAM", "PAM 认证", "已开启", "已关闭"),
		sshdYesNoRow(settings, "PermitUserEnvironment", "用户环境变量", "已允许", "已禁止", false),
		sshdYesNoRow(settings, "X11Forwarding", "X11 转发", "已开启", "已关闭", false),
		sshdYesNoRow(settings, "AllowTcpForwarding", "TCP 转发", "已开启", "已关闭", false),
		sshdValueRow(settings, "MaxAuthTries", "最大认证尝试次数", "信息", "未检测到"),
		sshdValueRow(settings, "AllowUsers", "允许登录用户", "信息", "未限制"),
		sshdValueRow(settings, "AllowGroups", "允许登录用户组", "信息", "未限制"),
	}
}

func sshdPortRow(settings sshdSettings) sshdSecurityRow {
	value := joinedSSHDSetting(settings, "Port")
	status := "信息"
	if value == "" {
		value = "未检测到"
		status = "未知"
	} else if value == "22" {
		status = "默认"
	} else {
		status = ""
	}
	return sshdSecurityRow{
		name:      "SSH 端口",
		value:     value,
		status:    status,
		directive: "Port",
	}
}

func sshdYesNoRow(settings sshdSettings, directive, name, yesText, noText string, secureWhenYes bool) sshdSecurityRow {
	value := firstSSHDSetting(settings, directive)
	display := value
	status := "未知"

	switch strings.ToLower(value) {
	case "yes":
		display = yesText
		if secureWhenYes {
			status = "安全"
		} else {
			status = "风险"
		}
	case "no":
		display = noText
		if secureWhenYes {
			status = "风险"
		} else {
			status = "安全"
		}
	case "":
		display = "未检测到"
	default:
		status = "信息"
	}

	return sshdSecurityRow{
		name:      name,
		value:     display,
		status:    status,
		directive: directive,
	}
}

func sshdYesNoInfoRow(settings sshdSettings, directive, name, yesText, noText string) sshdSecurityRow {
	row := sshdYesNoRow(settings, directive, name, yesText, noText, true)
	row.status = "信息"
	return row
}

func sshdKeyboardInteractiveRow(settings sshdSettings) sshdSecurityRow {
	row := sshdYesNoRow(settings, "KbdInteractiveAuthentication", "键盘交互认证", "已开启", "已禁用", false)
	if strings.EqualFold(firstSSHDSetting(settings, "KbdInteractiveAuthentication"), "yes") {
		row.status = "需确认"
	}
	return row
}

func sshdPermitRootLoginRow(settings sshdSettings) sshdSecurityRow {
	value := firstSSHDSetting(settings, "PermitRootLogin")
	display := value
	status := "未知"

	switch strings.ToLower(value) {
	case "no":
		display = "已禁止"
		status = "安全"
	case "prohibit-password", "without-password":
		display = "仅允许密钥登录"
		status = "安全"
	case "forced-commands-only":
		display = "仅允许强制命令"
		status = "安全"
	case "yes":
		display = "允许登录"
		status = "风险"
	case "":
		display = "未检测到"
	default:
		status = "信息"
	}

	return sshdSecurityRow{
		name:      "Root 登录",
		value:     display,
		status:    status,
		directive: "PermitRootLogin",
	}
}

func sshdValueRow(settings sshdSettings, directive, name, status, missing string) sshdSecurityRow {
	value := joinedSSHDSetting(settings, directive)
	if value == "" {
		value = missing
	}
	return sshdSecurityRow{
		name:      name,
		value:     value,
		status:    status,
		directive: directive,
	}
}

func firstSSHDSetting(settings sshdSettings, key string) string {
	values := settings[strings.ToLower(key)]
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

func joinedSSHDSetting(settings sshdSettings, key string) string {
	values := settings[strings.ToLower(key)]
	if len(values) == 0 {
		return ""
	}

	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			cleaned = append(cleaned, value)
		}
	}
	return strings.Join(cleaned, ", ")
}

func validateSSHDConfig(content string) error {
	tmp, err := os.CreateTemp("", "snail-sshd-*.conf")
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
	if err := system.Run(sshdCommandPath, "-t", "-f", tmp.Name()); err != nil {
		return fmt.Errorf("新 SSH 配置校验失败，未写入正式配置: %w", err)
	}
	return nil
}

func reloadSSHService() error {
	fmt.Println()
	log.Info("准备重新加载 SSH 服务...")

	switch {
	case system.SystemdUnitExists("ssh.service"):
		if err := system.Run("systemctl", "reload", "ssh"); err == nil {
			log.Info("SSH 服务已 reload：ssh.service")
			return nil
		}
		log.Warn("reload ssh.service 失败，尝试 restart")
		if err := system.Run("systemctl", "restart", "ssh"); err != nil {
			return err
		}
		log.Info("SSH 服务已 restart：ssh.service")
		return nil
	case system.SystemdUnitExists("sshd.service"):
		if err := system.Run("systemctl", "reload", "sshd"); err == nil {
			log.Info("SSH 服务已 reload：sshd.service")
			return nil
		}
		log.Warn("reload sshd.service 失败，尝试 restart")
		if err := system.Run("systemctl", "restart", "sshd"); err != nil {
			return err
		}
		log.Info("SSH 服务已 restart：sshd.service")
		return nil
	default:
		return fmt.Errorf("未找到 SSH 服务")
	}
}

func printSSHDConfig(path, content string) {
	fmt.Println()
	fmt.Printf("当前 SSH 配置文件：%s\n", path)
	fmt.Println("----------")
	fmt.Print(content)
	if !strings.HasSuffix(content, "\n") {
		fmt.Println()
	}
	fmt.Println("----------")
}
