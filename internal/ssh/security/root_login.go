package security

import (
	"fmt"
	"os"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
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

func ConfigureRootLogin(view *ui.UI) error {
	if !IsConfigured() {
		return fmt.Errorf("请先完成 SSH 安全策略配置，再单独调整 Root SSH 登录策略")
	}
	settings, _, err := readSettingsForReload()
	if err != nil {
		return err
	}
	permitRootLogin, err := promptRootLoginPolicy(view, settings)
	if err != nil {
		return err
	}
	if err := updateRootLoginPolicy(customSSHDConfigPath, permitRootLogin); err != nil {
		return err
	}
	log.Info("Root SSH 登录策略已更新；请保留当前会话，新开终端测试登录。")
	return nil
}

func promptRootLoginPolicy(view *ui.UI, settings sshdSettings) (string, error) {
	permitRootLogin := "prohibit-password"
	err := ui.RunPromptSteps(
		ui.PromptStep{Run: func() error {
			row := sshdPermitRootLoginRow(settings)
			ui.PrintField("当前 Root SSH 登录", row.value)
			ui.MenuOption("1", "仅允许 root 公钥登录")
			ui.MenuOption("2", "禁用 root SSH 登录（包括公钥）")
			ui.MenuExit("0/q", "返回 SSH 管理")
			for {
				choice, err := view.Ask("请选择：")
				if err != nil {
					return err
				}
				if shared.IsReturnChoice(choice) {
					return shared.ErrReturnToMenu
				}
				switch choice {
				case "1":
					permitRootLogin = "prohibit-password"
					return nil
				case "2":
					permitRootLogin = "no"
					return nil
				default:
					ui.InvalidChoice()
				}
			}
		}},
		ui.PromptStep{Run: func() error {
			var confirmed bool
			var err error
			if permitRootLogin == "no" {
				confirmed, err = confirmDisableRootLogin(view)
			} else {
				log.Warn("root 需有可用公钥才能登录；此操作不会添加公钥，也不会开放 root 密码登录。")
				confirmed, err = view.Confirm("确认仅允许 root 公钥登录？(y/N，0/q 返回上一步)：")
			}
			if err != nil {
				return err
			}
			if !confirmed {
				return ui.ErrCancelAction
			}
			return nil
		}},
	)
	return permitRootLogin, err
}

func confirmDisableRootLogin(view *ui.UI) (bool, error) {
	log.Warn("禁用后 root 将无法通过 SSH 新建连接，即使已配置公钥。工具无法验证普通用户是否已实际登录成功。")
	return view.Confirm("是否已在另一个终端使用普通用户公钥登录成功、确认 sudo 可用，并确认禁用 root SSH 登录？(y/N)：")
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
	options := shared.AtomicWriteOptions{Mode: info.Mode().Perm()}
	if err := shared.AtomicWriteFile(path, []byte(content), options); err != nil {
		return err
	}
	reloadAttempted := false
	defer func() {
		if err == nil {
			return
		}
		if restoreErr := shared.AtomicWriteFile(path, original, options); restoreErr != nil {
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
