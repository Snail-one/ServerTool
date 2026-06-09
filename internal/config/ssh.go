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
		file, err := os.OpenFile(authKeys, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
			if _, err := file.WriteString("\n"); err != nil {
				_ = file.Close()
				return err
			}
		}
		if _, err := file.WriteString(pubkey + "\n"); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Close(); err != nil {
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
	sshdConfig := "/etc/ssh/sshd_config"
	sshdDir := "/etc/ssh/sshd_config.d"
	customConf := filepath.Join(sshdDir, "99-custom.conf")

	if err := os.MkdirAll(sshdDir, 0755); err != nil {
		return err
	}

	fmt.Println()
	log.Info("检查 Include 配置...")
	data, err := os.ReadFile(sshdConfig)
	if err != nil {
		return err
	}
	includeRe := regexp.MustCompile(`(?m)^[[:space:]]*Include[[:space:]]+/etc/ssh/sshd_config\.d/\*\.conf`)
	if !includeRe.Match(data) {
		file, err := os.OpenFile(sshdConfig, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		if !strings.HasSuffix(string(data), "\n") {
			if _, err := file.WriteString("\n"); err != nil {
				_ = file.Close()
				return err
			}
		}
		if _, err := file.WriteString("Include /etc/ssh/sshd_config.d/*.conf\n"); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		log.Info("已自动添加 Include 配置")
	}

	if system.FileExists(customConf) {
		backup, err := system.Backup(customConf)
		if err != nil {
			return err
		}
		log.Info("已备份原 SSH 自定义配置：", backup)
	}

	fmt.Println()
	log.Info("写入自定义 SSH 配置...")
	content := fmt.Sprintf(`# Managed by setup tool

Port %d
PasswordAuthentication no
PermitRootLogin %s
PubkeyAuthentication yes
`, port, permitRootLogin)

	if err := os.WriteFile(customConf, []byte(content), 0644); err != nil {
		return err
	}
	if err := os.Chmod(customConf, 0644); err != nil {
		return err
	}

	fmt.Println()
	log.Info("验证 sshd 配置...")
	if err := system.Run("/usr/sbin/sshd", "-t"); err != nil {
		return fmt.Errorf("sshd 配置校验失败: %w", err)
	}
	return nil
}

func reloadSSHService() error {
	fmt.Println()
	log.Info("重新加载 SSH 服务...")

	switch {
	case system.SystemdUnitExists("ssh.service"):
		if err := system.Run("systemctl", "reload", "ssh"); err == nil {
			return nil
		}
		return system.Run("systemctl", "restart", "ssh")
	case system.SystemdUnitExists("sshd.service"):
		if err := system.Run("systemctl", "reload", "sshd"); err == nil {
			return nil
		}
		return system.Run("systemctl", "restart", "sshd")
	default:
		return fmt.Errorf("未找到 SSH 服务")
	}
}

func containsLine(content, wanted string) bool {
	for _, line := range strings.Split(content, "\n") {
		if line == wanted {
			return true
		}
	}
	return false
}
