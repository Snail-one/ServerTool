package clientproxy

import (
	"bytes"
	"fmt"

	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

func Remove(view *ui.UI) error {
	account, err := system.CurrentTargetUser()
	if err != nil {
		return err
	}
	return remove(view, account)
}

func remove(view promptReader, account *system.Account) error {
	path, err := ConfigPath(account)
	if err != nil {
		return err
	}
	doc, original, exists, err := readDocument(path)
	if err != nil {
		return err
	}
	if !exists || !doc.hasProxy() {
		ui.PrintInfoCard("无需移除 Docker 构建与容器代理", ui.CardField{Label: "配置文件", Value: path})
		return nil
	}
	if err := requireManagedConfig(path, original); err != nil {
		return err
	}
	ui.PrintInfoCard("移除 Docker 构建与容器代理",
		ui.CardField{Label: "目标用户", Value: account.Name},
		ui.CardField{Label: "配置文件", Value: path},
		ui.CardField{Label: "当前代理", Value: doc.summary()},
		ui.CardField{Label: "操作范围", Value: "移除默认 HTTP/HTTPS 代理及绕过列表；用于后续构建及新建容器，无需重启 Docker"},
	)
	ok, err := view.Confirm("确认移除 Docker 构建与容器代理？(y/N)：")
	if err != nil {
		return err
	}
	if !ok {
		return shared.ErrReturnToMenu
	}
	latest, latestExists, err := shared.ReadRegularFile(path)
	if err != nil {
		return err
	}
	if !latestExists || !bytes.Equal(latest, original) {
		return fmt.Errorf("Docker 客户端配置在操作期间发生变化，未移除，请重试: %s", path)
	}
	backup, err := managedStore.Snapshot(path)
	if err != nil {
		return fmt.Errorf("备份 Docker 客户端配置失败，未移除: %w", err)
	}
	if err := Cleanup(account); err != nil {
		return err
	}
	ui.PrintSuccessCard("Docker 构建与容器代理已移除",
		ui.CardField{Label: "配置文件", Value: path},
		ui.CardField{Label: "备份文件", Value: backup},
		ui.CardField{Label: "生效范围", Value: "后续构建及新建容器；已有容器保持当前配置，无需重启 Docker"},
	)
	return nil
}
