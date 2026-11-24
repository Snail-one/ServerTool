package configuration

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"snail_tool/internal/log"
	"snail_tool/internal/storage"
	"snail_tool/internal/tempguard"
	"snail_tool/internal/ui"
)

func (store Store) Edit(commandExists func(string) bool, commandRun func(string, ...string) error) error {
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
	before, err := store.Read()
	if err != nil {
		return err
	}
	if err := store.PreserveOriginal(); err != nil {
		return err
	}
	edited, err := store.editCopy(editor, before.Content, commandRun)
	if err != nil {
		return err
	}
	if bytes.Equal(before.Content, edited) {
		log.Info("配置内容未修改，无需备份或加载")
		return nil
	}
	if err := store.Check(before); err != nil {
		return err
	}
	if before.Exists {
		backup, err := store.Backup()
		if err != nil {
			return err
		}
		log.Info("已备份原防火墙配置：", backup)
	}
	if err := storage.AtomicWrite(store.Path, storage.AtomicWriteOptions{Mode: 0644}, func(w io.Writer) error {
		if err := store.Check(before); err != nil {
			return err
		}
		_, err := w.Write(edited)
		return err
	}); err != nil {
		return err
	}
	after := Version{Content: edited, Exists: true}
	if err := store.Validate(after); err != nil {
		return fmt.Errorf("编辑内容已保留，请修正配置后重新保存: %w", err)
	}
	if err := store.loadAndShow(after.Content); err != nil {
		return fmt.Errorf("编辑内容已保留，可输入 r 重试加载: %w", err)
	}
	return nil
}

func (store Store) editCopy(editor string, original []byte, commandRun func(string, ...string) error) ([]byte, error) {
	release, err := tempguard.Use()
	if err != nil {
		return nil, err
	}
	defer release()

	if err := storage.EnsurePrivateDir(store.DataDir); err != nil {
		return nil, err
	}
	dir := filepath.Join(store.DataDir, storage.DataTempDirName)
	if err := storage.EnsurePrivateDir(dir); err != nil {
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
	edited, exists, err := storage.ReadRegularFile(draft.Name())
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("编辑副本已被删除")
	}
	return edited, nil
}
