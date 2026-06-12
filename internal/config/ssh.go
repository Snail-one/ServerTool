package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

const (
	managedSSHDConfigHeader = "# Managed by setup tool"
	managedSSHDIncludeBegin = "# ===== BEGIN SNAIL SSH INCLUDE ====="
	managedSSHDIncludeEnd   = "# ===== END SNAIL SSH INCLUDE ====="
	sshAuthorizedKeysBegin  = "# ===== BEGIN SNAIL SSH AUTHORIZED KEYS ====="
	sshAuthorizedKeysEnd    = "# ===== END SNAIL SSH AUTHORIZED KEYS ====="
	sshdConfigPath          = "/etc/ssh/sshd_config"
	sshdConfigDir           = "/etc/ssh/sshd_config.d"
	customSSHDConfigPath    = "/etc/ssh/sshd_config.d/99-custom.conf"
)

func ConfigureSSH(view *ui.UI) error {
	account, err := system.CurrentTargetUser()
	if err != nil {
		return err
	}

	log.Info("当前配置用户：", account.Name)
	fmt.Println()

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

	if err := printAuthorizedKeys(account); err != nil {
		return err
	}

	pubkey, err := view.Ask("请粘贴 SSH 公钥: ")
	if err != nil {
		return err
	}
	if err := system.ValidateSSHPublicKey(pubkey); err != nil {
		return err
	}

	if err := installAuthorizedKey(account, pubkey); err != nil {
		return err
	}

	shouldWriteConfig, err := confirmSSHDConfigOverwrite(view)
	if err != nil {
		return err
	}
	if !shouldWriteConfig {
		fmt.Println()
		log.Info("SSH 配置完成")
		fmt.Println()
		fmt.Printf("用户：%s\n", account.Name)
		fmt.Println("SSH 公钥：已确认")
		fmt.Println("SSH 服务配置：保留现有配置，未重新加载服务")
		fmt.Println()
		log.Warn("请先新开一个终端测试 SSH 登录成功后，再关闭当前会话。")
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
	log.Info("SSH 配置完成")
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

func confirmSSHDConfigOverwrite(view *ui.UI) (bool, error) {
	if !system.FileExists(customSSHDConfigPath) {
		return true, nil
	}

	existing, err := os.ReadFile(customSSHDConfigPath)
	if err != nil {
		return false, err
	}
	if !strings.Contains(string(existing), managedSSHDConfigHeader) {
		return true, nil
	}

	printSSHDConfig(customSSHDConfigPath, string(existing))
	return view.Confirm("检测到该 SSH 配置文件由脚本创建，是否覆盖并重新生成？(y/N): ")
}

func printAuthorizedKeys(account *system.Account) error {
	authKeys := filepath.Join(account.Home, ".ssh", "authorized_keys")
	if !system.FileExists(authKeys) {
		return nil
	}

	data, err := os.ReadFile(authKeys)
	if err != nil {
		return err
	}

	content := strings.TrimSpace(string(data))
	if content == "" {
		return nil
	}

	fmt.Println("当前已存在的 SSH 公钥配置：")
	fmt.Println("----------")
	fmt.Println(content)
	fmt.Println("----------")
	fmt.Println()
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
	if err := system.Run("/usr/sbin/sshd", "-t"); err != nil {
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

func buildSSHDConfig(port int, permitRootLogin string) string {
	return fmt.Sprintf(`%s

Port %d
PasswordAuthentication no
PermitRootLogin %s
PubkeyAuthentication yes
`, managedSSHDConfigHeader, port, permitRootLogin)
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
	if err := system.Run("/usr/sbin/sshd", "-t", "-f", tmp.Name()); err != nil {
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
