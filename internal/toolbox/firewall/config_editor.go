package firewall

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/tempguard"
	"snail_tool/internal/ui"
)

func editConfig() error {
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "编辑配置文件")
	var editor string
	for _, name := range []string{"vim", "vi", "nano"} {
		if commandExists(name) {
			editor = name
			break
		}
	}
	if editor == "" {
		return fmt.Errorf("未找到 vim、vi、nano 中的任意工具，请先安装其中一个编辑器")
	}
	before, err := readConfigVersion()
	if err != nil {
		return err
	}
	if err := preserveOriginal(); err != nil {
		return err
	}
	edited, err := editConfigCopy(editor, before.content)
	if err != nil {
		return err
	}
	if bytes.Equal(before.content, edited) {
		log.Info("配置内容未修改，无需备份或加载")
		return nil
	}
	if err := checkConfigVersion(before); err != nil {
		return err
	}
	if before.exists {
		backup, err := backupFirewallConfig()
		if err != nil {
			return err
		}
		log.Info("已备份原防火墙配置：", backup)
	}
	if err := shared.AtomicWrite(configPath, shared.AtomicWriteOptions{Mode: 0644}, func(w io.Writer) error {
		if err := checkConfigVersion(before); err != nil {
			return err
		}
		_, err := w.Write(edited)
		return err
	}); err != nil {
		return err
	}
	after := configVersion{content: edited, exists: true}
	if err := validateSavedConfig(after); err != nil {
		return fmt.Errorf("编辑内容已保留，请修正配置后重新保存: %w", err)
	}
	if err := loadValidatedConfig(after.content); err != nil {
		return fmt.Errorf("编辑内容已保留，可输入 r 重试加载: %w", err)
	}
	return nil
}

func editConfigCopy(editor string, original []byte) ([]byte, error) {
	release, err := tempguard.Use()
	if err != nil {
		return nil, err
	}
	defer release()

	if err := shared.EnsurePrivateDir(dataDir); err != nil {
		return nil, err
	}
	dir := filepath.Join(dataDir, shared.DataTempDirName)
	if err := shared.EnsurePrivateDir(dir); err != nil {
		return nil, err
	}
	draft, err := os.CreateTemp(dir, ".snail-nftables-edit-*.conf")
	if err != nil {
		return nil, fmt.Errorf("创建防火墙编辑副本失败: %w", err)
	}
	defer os.Remove(draft.Name())
	if _, err := draft.Write(original); err != nil {
		_ = draft.Close()
		return nil, err
	}
	if err := draft.Close(); err != nil {
		return nil, err
	}
	if err := commandRun(editor, draft.Name()); err != nil {
		return nil, fmt.Errorf("%s 编辑失败: %w", editor, err)
	}
	edited, exists, err := shared.ReadRegularFile(draft.Name())
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("编辑副本已被删除")
	}
	return edited, nil
}
