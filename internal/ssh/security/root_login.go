package security

import (
	"fmt"
	"os"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/storage"
	"snail_tool/internal/system"
)

var (
	validateRootLoginConfig = validateSSHDConfig
	checkRootLoginConfig    = func() error { return system.Run(sshdCommandPath, "-t") }
)

// Keep an existing restriction when hardening again, regardless of target user.
func rootLoginForHardening(settings sshdSettings) string {
	switch value := firstSSHDSetting(settings, "PermitRootLogin"); value {
	case "no", "prohibit-password", "without-password", "forced-commands-only":
		return value
	default:
		return "prohibit-password"
	}
}

// Change only the managed root directive; leave ports and other settings intact.
func replaceManagedRootLogin(content, value string) (string, error) {
	if value != "no" && value != "prohibit-password" {
		return "", fmt.Errorf("不支持的 Root SSH 登录策略: %s", value)
	}
	if strings.Count(content, managedSSHDConfigBegin) != 1 || strings.Count(content, managedSSHDConfigEnd) != 1 {
		return "", fmt.Errorf("SSH 托管区块缺失或重复，请先重新配置 SSH 安全策略")
	}
	begin := strings.Index(content, managedSSHDConfigBegin) + len(managedSSHDConfigBegin)
	end := strings.Index(content, managedSSHDConfigEnd)
	if end < begin {
		return "", fmt.Errorf("SSH 托管区块顺序异常")
	}
	lines := strings.Split(content[begin:end], "\n")
	replaced := false
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if strings.EqualFold(fields[0], "Match") {
			return "", fmt.Errorf("SSH 托管区块含 Match 配置，请先重新配置 SSH 安全策略")
		}
		if strings.EqualFold(fields[0], "PermitRootLogin") {
			lines[i] = ""
			if !replaced {
				lines[i] = "PermitRootLogin " + value
				replaced = true
			}
		}
	}
	if !replaced {
		return "", fmt.Errorf("SSH 托管区块缺少 PermitRootLogin，请先重新配置 SSH 安全策略")
	}
	return content[:begin] + strings.Join(lines, "\n") + content[end:], nil
}

func updateRootLoginPolicy(path, value string) (err error) {
	original, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	content, err := replaceManagedRootLogin(string(original), value)
	if err != nil {
		return err
	}
	if err := validateRootLoginConfig(content); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	backup, err := backupStore.Snapshot(path)
	if err != nil {
		return err
	}
	log.Info("已备份 SSH 配置：", backup)
	options := storage.AtomicWriteOptions{Mode: info.Mode().Perm()}
	if err := storage.AtomicWriteFile(path, []byte(content), options); err != nil {
		return err
	}
	reloadAttempted := false
	defer func() {
		if err == nil {
			return
		}
		if restoreErr := storage.AtomicWriteFile(path, original, options); restoreErr != nil {
			err = fmt.Errorf("%w；恢复原配置失败: %v；备份: %s", err, restoreErr, backup)
			return
		}
		if reloadAttempted {
			if reloadErr := reloadConfiguredSSH(); reloadErr != nil {
				err = fmt.Errorf("%w；原配置文件已恢复，但重新加载失败: %v", err, reloadErr)
				return
			}
		}
		err = fmt.Errorf("%w；已恢复原配置", err)
	}()
	if err = checkRootLoginConfig(); err != nil {
		return err
	}
	settings, _, err := readSettingsForReload()
	if err != nil {
		return err
	}
	effective := firstSSHDSetting(settings, "PermitRootLogin")
	if effective == "without-password" {
		effective = "prohibit-password"
	}
	if effective != value {
		return fmt.Errorf("其他 SSH 配置覆盖了 Root 登录策略（读取到 %s），未重载 SSH", effective)
	}
	reloadAttempted = true
	return reloadConfiguredSSH()
}

func CurrentRootLogin() (string, error) {
	if !IsConfigured() {
		return "", fmt.Errorf("请先完成 SSH 安全策略配置，再单独调整 Root SSH 登录策略")
	}
	settings, _, err := readSettingsForReload()
	if err != nil {
		return "", err
	}
	return sshdPermitRootLoginRow(settings).value, nil
}

func UpdateRootLoginPolicy(value string) error {
	return updateRootLoginPolicy(customSSHDConfigPath, value)
}
