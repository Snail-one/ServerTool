package vscodeserver

import (
	"fmt"
	"os"
	"path/filepath"

	"snail_tool/internal/storage"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

var installDirectories = [...]string{".vscode-server", ".vscode-server-insiders", ".vscode-remote"}

type installation struct {
	path string
	info os.FileInfo
}

func Run(view *ui.UI, account *system.Account) error {
	targets, err := inspectInstallations(account.Home)
	if err != nil {
		return err
	}
	paths := make([]string, 0, len(targets))
	fields := []ui.CardField{{Label: "目标用户", Value: account.Name}}
	hasInstallation := false
	for _, target := range targets {
		paths = append(paths, target.path)
		state := "将删除"
		if target.info == nil {
			state = "不存在"
		} else {
			hasInstallation = true
		}
		fields = append(fields, ui.CardField{Label: state, Value: target.path})
	}
	processes := remoteProcesses{uid: account.UID, roots: paths}
	pids, err := processes.list()
	if err != nil {
		return err
	}
	ui.ClearScreen()
	ui.MenuTitle("清理配置", "彻底删除 VS Code Server")
	if len(pids) == 0 && !hasInstallation {
		ui.PrintInfoCard("未发现 VS Code Server", fields...)
		return nil
	}
	fields = append(fields,
		ui.CardField{Label: "检测到的服务端进程", Value: fmt.Sprintf("%d", len(pids))},
		ui.CardField{Label: "删除内容", Value: "服务端程序、远程扩展、远程设置、缓存和日志"},
		ui.CardField{Label: "会话影响", Value: "将断开该用户的 VS Code 远程会话，请在普通 SSH 或本机终端运行"},
	)
	ui.PrintDangerCard("永久删除 VS Code Server", fields...)
	confirmed, err := view.Confirm("确认停止 VS Code Server 并永久删除上述安装目录？(y/N)：")
	if err != nil {
		return err
	}
	if !confirmed {
		return ui.ErrReturnToMenu
	}

	if err := verifyInstallations(targets); err != nil {
		return err
	}
	if err := processes.stop(); err != nil {
		return err
	}
	if err := verifyInstallations(targets); err != nil {
		return err
	}
	for _, target := range targets {
		if target.info == nil {
			continue
		}
		if err := target.verify(); err != nil {
			return err
		}
		if err := storage.RemovePathTree(target.path); err != nil {
			return fmt.Errorf("VS Code Server 目录删除未完成: %w", err)
		}
	}
	ui.PrintSuccessCard("VS Code Server 已删除", ui.CardField{Label: "目标用户", Value: account.Name})
	return nil
}

func inspectInstallations(home string) ([]installation, error) {
	if !filepath.IsAbs(home) || filepath.Clean(home) != home || home == string(os.PathSeparator) {
		return nil, fmt.Errorf("目标用户的 home 路径无效: %q", home)
	}
	targets := make([]installation, 0, len(installDirectories))
	for _, name := range installDirectories {
		path := filepath.Join(home, name)
		info, err := inspectDirectory(path)
		if err != nil {
			return nil, err
		}
		targets = append(targets, installation{path: path, info: info})
	}
	return targets, nil
}

func inspectDirectory(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if info != nil && !info.IsDir() {
		return nil, fmt.Errorf("VS Code Server 路径不是普通目录: %s", path)
	}
	if err := storage.CheckRemovalTree(path); err != nil {
		return nil, err
	}
	return info, nil
}

func verifyInstallations(targets []installation) error {
	for _, target := range targets {
		if err := target.verify(); err != nil {
			return err
		}
	}
	return nil
}

func (target installation) verify() error {
	info, err := inspectDirectory(target.path)
	if err != nil {
		return err
	}
	if (target.info == nil && info == nil) || (target.info != nil && info != nil && os.SameFile(target.info, info)) {
		return nil
	}
	return fmt.Errorf("VS Code Server 目录自确认以来发生变化，请重新检查: %s", target.path)
}
