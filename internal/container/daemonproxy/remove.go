package daemonproxy

import (
	"bytes"
	"fmt"
	"strings"

	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

type confirmer interface {
	Confirm(string) (bool, error)
}

// RemovalPlan preserves unrelated systemd settings and verifies that the file
// has not changed between the confirmation/preflight and the actual removal.
type RemovalPlan struct {
	path     string
	original []byte
	cleaned  []byte
	remove   bool
	store    shared.BackupStore
}

// PrepareRemoval returns nil when the drop-in contains no supported proxy
// settings. The shared Docker cleanup uses the same parsing and ownership rules.
func PrepareRemoval(path string, store shared.BackupStore) (*RemovalPlan, error) {
	content, exists, err := shared.ReadRegularFile(path)
	if err != nil || !exists {
		return nil, err
	}
	var kept []string
	changed := false
	for _, line := range strings.SplitAfter(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		proxy := false
		for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY"} {
			if strings.HasPrefix(trimmed, `Environment="`+name+`=`) {
				if !strings.HasSuffix(trimmed, `"`) || strings.Count(trimmed, `"`) != 2 {
					return nil, fmt.Errorf("Docker 代理配置格式复杂，未移除，请手动检查: %s", path)
				}
				proxy = true
			}
		}
		if proxy {
			changed = true
		} else {
			kept = append(kept, line)
		}
	}
	if !changed {
		return nil, nil
	}
	managed, err := store.MatchesManaged(path, content)
	if err != nil {
		return nil, err
	}
	if !managed {
		return nil, fmt.Errorf("Docker 服务代理配置没有本工具写入记录或已被修改，已保留，请手动检查: %s", path)
	}
	cleaned := strings.Join(kept, "")
	meaningful := false
	for _, line := range strings.Split(cleaned, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && line != "[Service]" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, ";") {
			meaningful = true
		}
	}
	return &RemovalPlan{path: path, original: content, cleaned: []byte(cleaned), remove: !meaningful, store: store}, nil
}

func (plan *RemovalPlan) checkUnchanged() error {
	content, exists, err := shared.ReadRegularFile(plan.path)
	if err != nil {
		return err
	}
	if !exists || !bytes.Equal(content, plan.original) {
		return fmt.Errorf("Docker 服务代理配置在操作期间发生变化，未移除，请重试: %s", plan.path)
	}
	return nil
}

func (plan *RemovalPlan) Apply() error {
	if err := plan.checkUnchanged(); err != nil {
		return err
	}
	var err error
	if plan.remove {
		err = shared.RemoveRegularFile(plan.path)
	} else {
		err = shared.AtomicWriteFile(plan.path, plan.cleaned, shared.AtomicWriteOptions{Mode: 0644})
	}
	if err != nil {
		return err
	}
	if err := plan.store.ForgetManaged(plan.path); err != nil {
		return fmt.Errorf("Docker 服务代理已移除，但移除写入记录失败: %w", err)
	}
	return nil
}

func Remove(view *ui.UI) error {
	return remove(view, dockerProxyPath, system.IsRoot, system.SystemdUnitExists, system.Run)
}

func remove(view confirmer, path string, isRoot func() bool, unitExists func(string) bool, run func(string, ...string) error) error {
	if !isRoot() {
		return fmt.Errorf("移除 Docker 服务代理需要 root 权限，请使用 sudo 运行本工具")
	}
	plan, err := PrepareRemoval(path, managedStore)
	if err != nil {
		return err
	}
	if plan == nil {
		ui.PrintInfoCard("无需移除 Docker 服务代理", ui.CardField{Label: "配置文件", Value: path})
		return nil
	}
	hasService := unitExists("docker.service")
	impact := "重新加载 systemd；移除本工具管理的 HTTP/HTTPS 代理及绕过列表"
	if hasService {
		impact += "；尝试重启 Docker，可能影响运行中容器"
	}
	ui.PrintDangerCard("移除 Docker 服务代理",
		ui.CardField{Label: "配置文件", Value: path},
		ui.CardField{Label: "当前代理", Value: summarizeProxyConfig(path)},
		ui.CardField{Label: "操作范围", Value: impact},
	)
	ok, err := view.Confirm("确认移除 Docker 服务代理？(y/N)：")
	if err != nil {
		return err
	}
	if !ok {
		return shared.ErrReturnToMenu
	}
	if err := plan.checkUnchanged(); err != nil {
		return err
	}
	backup, err := managedStore.Snapshot(path)
	if err != nil {
		return fmt.Errorf("备份 Docker 服务代理失败，未移除: %w", err)
	}
	if err := plan.Apply(); err != nil {
		return err
	}
	if err := run("systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("Docker 服务代理已移除，但 systemctl daemon-reload 失败: %w", err)
	}
	if hasService {
		if err := run("systemctl", "try-restart", "docker.service"); err != nil {
			return fmt.Errorf("Docker 服务代理已移除，但应用配置失败: %w", err)
		}
	}
	ui.PrintSuccessCard("Docker 服务代理已移除",
		ui.CardField{Label: "配置文件", Value: path},
		ui.CardField{Label: "备份文件", Value: backup},
	)
	return nil
}
