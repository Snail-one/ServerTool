package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

type authorizedKeyEntry struct {
	index   int
	line    string
	managed bool
}

type sshdSettings map[string][]string

type sshdSecurityRow struct {
	name      string
	value     string
	status    string
	directive string
}

const (
	managedSSHDConfigHeader = "# Managed by setup tool"
	managedSSHDIncludeBegin = "# ===== BEGIN SNAIL SSH INCLUDE ====="
	managedSSHDIncludeEnd   = "# ===== END SNAIL SSH INCLUDE ====="
	sshAuthorizedKeysBegin  = "# ===== BEGIN SNAIL SSH AUTHORIZED KEYS ====="
	sshAuthorizedKeysEnd    = "# ===== END SNAIL SSH AUTHORIZED KEYS ====="
	sshdConfigPath          = "/etc/ssh/sshd_config"
	sshdConfigDir           = "/etc/ssh/sshd_config.d"
	customSSHDConfigPath    = "/etc/ssh/sshd_config.d/99-custom.conf"
	sshdCommandPath         = "/usr/sbin/sshd"
)

func ConfigureSSH(view *ui.UI) error {
	account, err := system.CurrentTargetUser()
	if err != nil {
		return err
	}

	log.Info("当前配置用户：", account.Name)
	fmt.Println()
	return configureSSHAuthorizedKeys(view, account)
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

func ShowSSHSecurityStatus() error {
	settings, source, err := loadSSHDSettings()
	if err != nil {
		return err
	}

	fmt.Println("当前 SSH 安全配置：")
	fmt.Println()
	fmt.Println("配置来源：", source)
	fmt.Println()
	for _, row := range buildSSHSecurityRows(settings) {
		fmt.Printf("- %s：%s [%s]\n", row.name, row.value, row.status)
		if row.directive != "" {
			fmt.Printf("  配置项：%s\n", row.directive)
		}
	}
	return nil
}

func configureSSHAuthorizedKeys(view *ui.UI, account *system.Account) error {
	for {
		if err := printAuthorizedKeys(account); err != nil {
			return err
		}

		fmt.Println("请选择公钥操作：")
		fmt.Println("1) 添加公钥")
		fmt.Println("2) 删除公钥")
		fmt.Println("0/q) 返回")
		fmt.Println()

		choice, err := view.Ask("输入选项: ")
		if err != nil {
			return err
		}
		fmt.Println()

		switch strings.ToLower(choice) {
		case "1":
			if err := addSSHAuthorizedKeys(view, account); err != nil {
				return err
			}
		case "2":
			if err := deleteSSHAuthorizedKeys(view, account); err != nil {
				return err
			}
		case "0", "q", "exit":
			return ErrReturnToMenu
		default:
			fmt.Println("无效选项，请重新输入")
		}
		fmt.Println()
	}
}

func addSSHAuthorizedKeys(view *ui.UI, account *system.Account) error {
	for {
		pubkey, err := view.Ask("请粘贴 SSH 公钥（直接回车结束）: ")
		if err != nil {
			return err
		}
		if strings.TrimSpace(pubkey) == "" {
			return nil
		}
		if err := system.ValidateSSHPublicKey(pubkey); err != nil {
			return err
		}

		if err := installAuthorizedKey(account, pubkey); err != nil {
			return err
		}
		fmt.Println()
	}
}

func deleteSSHAuthorizedKeys(view *ui.UI, account *system.Account) error {
	authKeys := filepath.Join(account.Home, ".ssh", "authorized_keys")
	if !system.FileExists(authKeys) {
		log.Info("未发现 SSH authorized_keys，跳过")
		return nil
	}

	data, err := os.ReadFile(authKeys)
	if err != nil {
		return err
	}

	content := string(data)
	entries := authorizedKeyEntries(content)
	if len(entries) == 0 {
		log.Info("未发现可删除的 SSH 公钥")
		return nil
	}

	printAuthorizedKeyEntries(entries)
	rawSelection, err := view.Ask("请输入要删除的编号（多个用逗号或空格分隔，直接回车返回）: ")
	if err != nil {
		return err
	}
	if strings.TrimSpace(rawSelection) == "" {
		return nil
	}

	indexes, err := parseAuthorizedKeySelection(rawSelection, len(entries))
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("即将删除以下 SSH 公钥：")
	for _, index := range indexes {
		entry := entries[index-1]
		fmt.Printf("%d) %s\n", entry.index, summarizeAuthorizedKey(entry.line))
	}
	fmt.Println()

	confirmed, err := view.Confirm("确认删除选中的 SSH 公钥？(y/N): ")
	if err != nil {
		return err
	}
	if !confirmed {
		fmt.Println("已取消删除")
		return nil
	}

	selected := make(map[int]struct{}, len(indexes))
	for _, index := range indexes {
		selected[index] = struct{}{}
	}
	cleaned := removeAuthorizedKeyIndexes(content, selected)
	if err := os.WriteFile(authKeys, []byte(cleaned), 0600); err != nil {
		return err
	}
	if err := os.Chmod(authKeys, 0600); err != nil {
		return err
	}
	if err := system.ChownPath(authKeys, account, false); err != nil {
		return err
	}

	log.Info("已删除选中的 SSH 公钥")
	return nil
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

	if err := writeSSHDConfig(port, permitRootLogin); err != nil {
		return err
	}
	if err := reloadSSHService(); err != nil {
		return err
	}

	fmt.Println()
	log.Info("SSH 服务安全配置完成")
	fmt.Println()
	fmt.Printf("用户：%s\n", account.Name)
	fmt.Printf("端口：%d\n", port)
	fmt.Printf("PermitRootLogin：%s\n", permitRootLogin)
	fmt.Println()
	fmt.Println("连接方式：")
	fmt.Printf("ssh -p %d %s@服务器IP\n", port, account.Name)
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
	if !strings.Contains(existingContent, managedSSHDConfigHeader) {
		printSSHDConfig(customSSHDConfigPath, existingContent)
		return view.Confirm("检测到该 SSH 配置文件不是本工具生成，继续会备份并覆盖，是否继续？(y/N): ")
	}

	printSSHDConfig(customSSHDConfigPath, existingContent)
	return view.Confirm("检测到该 SSH 配置文件由脚本创建，是否覆盖并重新生成？(y/N): ")
}

func printAuthorizedKeys(account *system.Account) error {
	authKeys := filepath.Join(account.Home, ".ssh", "authorized_keys")
	if !system.FileExists(authKeys) {
		log.Info("未发现 SSH authorized_keys")
		return nil
	}

	data, err := os.ReadFile(authKeys)
	if err != nil {
		return err
	}

	entries := authorizedKeyEntries(string(data))
	if len(entries) == 0 {
		log.Info("未发现 SSH 公钥")
		return nil
	}

	printAuthorizedKeyEntries(entries)
	return nil
}

func installAuthorizedKey(account *system.Account, pubkey string) error {
	sshDir := filepath.Join(account.Home, ".ssh")
	authKeys := filepath.Join(sshDir, "authorized_keys")

	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return err
	}
	if err := ensureFile(authKeys); err != nil {
		return err
	}

	data, err := os.ReadFile(authKeys)
	if err != nil {
		return err
	}
	if !containsLine(string(data), pubkey) {
		if err := writeManagedAuthorizedKey(authKeys, string(data), pubkey); err != nil {
			return err
		}
		log.Info("已添加 SSH 公钥")
	} else {
		log.Info("SSH 公钥已存在，跳过添加")
	}

	if err := os.Chmod(sshDir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(authKeys, 0600); err != nil {
		return err
	}
	return system.ChownPath(sshDir, account, true)
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

func writeSSHDConfig(port int, permitRootLogin string) error {
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

	fmt.Println()
	log.Info("验证当前 sshd 配置...")
	if err := system.Run(sshdCommandPath, "-t"); err != nil {
		return fmt.Errorf("sshd 配置校验失败: %w", err)
	}
	return nil
}

func writeManagedAuthorizedKey(path, content, pubkey string) error {
	keys := managedAuthorizedKeys(content)
	keys = append(keys, pubkey)

	cleaned := removeManagedBlock(content, sshAuthorizedKeysBegin, sshAuthorizedKeysEnd)
	block := fmt.Sprintf("%s\n%s\n%s\n", sshAuthorizedKeysBegin, strings.Join(keys, "\n"), sshAuthorizedKeysEnd)
	return os.WriteFile(path, []byte(appendBlock(cleaned, block)), 0600)
}

func managedAuthorizedKeys(content string) []string {
	block, ok := managedBlockContent(content, sshAuthorizedKeysBegin, sshAuthorizedKeysEnd)
	if !ok {
		return nil
	}

	var keys []string
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		keys = append(keys, line)
	}
	return keys
}

func authorizedKeyEntries(content string) []authorizedKeyEntry {
	var entries []authorizedKeyEntry
	managed := false
	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimRight(rawLine, "\r")
		trimmed := strings.TrimSpace(line)
		switch trimmed {
		case sshAuthorizedKeysBegin:
			managed = true
			continue
		case sshAuthorizedKeysEnd:
			managed = false
			continue
		}
		if !isAuthorizedKeyLine(trimmed) {
			continue
		}
		entries = append(entries, authorizedKeyEntry{
			index:   len(entries) + 1,
			line:    line,
			managed: managed,
		})
	}
	return entries
}

func printAuthorizedKeyEntries(entries []authorizedKeyEntry) {
	fmt.Println("当前 SSH 公钥：")
	for _, entry := range entries {
		source := "手动"
		if entry.managed {
			source = "本工具"
		}
		fmt.Printf("%d) [%s] %s\n", entry.index, source, summarizeAuthorizedKey(entry.line))
	}
	fmt.Println()
}

func parseAuthorizedKeySelection(raw string, max int) ([]int, error) {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	if len(parts) == 0 {
		return nil, fmt.Errorf("未选择要删除的 SSH 公钥编号")
	}

	indexes := make([]int, 0, len(parts))
	seen := make(map[int]struct{}, len(parts))
	for _, part := range parts {
		index, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("无效公钥编号: %s", part)
		}
		if index < 1 || index > max {
			return nil, fmt.Errorf("公钥编号超出范围: %d", index)
		}
		if _, ok := seen[index]; ok {
			continue
		}
		seen[index] = struct{}{}
		indexes = append(indexes, index)
	}
	return indexes, nil
}

func removeAuthorizedKeyIndexes(content string, selected map[int]struct{}) string {
	lines := strings.SplitAfter(content, "\n")
	var builder strings.Builder
	index := 0
	for _, line := range lines {
		if line == "" {
			continue
		}
		trimmed := strings.TrimSpace(strings.TrimRight(line, "\r\n"))
		if isAuthorizedKeyLine(trimmed) {
			index++
			if _, ok := selected[index]; ok {
				continue
			}
		}
		builder.WriteString(line)
	}

	cleaned := removeEmptyManagedAuthorizedKeyBlock(builder.String())
	return normalizeCleanedContent(cleaned)
}

func removeEmptyManagedAuthorizedKeyBlock(content string) string {
	block, ok := managedBlockContent(content, sshAuthorizedKeysBegin, sshAuthorizedKeysEnd)
	if ok && len(authorizedKeyEntries(block)) == 0 {
		return removeManagedBlock(content, sshAuthorizedKeysBegin, sshAuthorizedKeysEnd)
	}
	return content
}

func isAuthorizedKeyLine(trimmed string) bool {
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return false
	}
	return true
}

func summarizeAuthorizedKey(line string) string {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return truncateString(strings.TrimSpace(line), 90)
	}

	keyBody := fields[1]
	if len(keyBody) > 24 {
		keyBody = keyBody[:16] + "..." + keyBody[len(keyBody)-8:]
	}

	summary := fields[0] + " " + keyBody
	if len(fields) > 2 {
		summary += " " + strings.Join(fields[2:], " ")
	}
	return truncateString(summary, 120)
}

func truncateString(value string, max int) string {
	if len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return value[:max-3] + "..."
}

func buildSSHDConfig(port int, permitRootLogin string) string {
	return fmt.Sprintf(`%s

Port %d
PasswordAuthentication no
PermitRootLogin %s
PubkeyAuthentication yes
`, managedSSHDConfigHeader, port, permitRootLogin)
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
		status = "已调整"
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

func containsLine(content, wanted string) bool {
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == wanted {
			return true
		}
	}
	return false
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
