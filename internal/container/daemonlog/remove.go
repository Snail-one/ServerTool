package daemonlog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

type removalConfirmer interface {
	Confirm(string) (bool, error)
}

// RemovalPlan is shared with the full Docker cleanup so both entry points use
// the same ownership checks and preserve settings outside log-driver/log-opts.
type RemovalPlan struct {
	path     string
	original []byte
	cleaned  []byte
	status   dockerLogStatus
	store    shared.BackupStore
}

func PrepareRemoval(path string, store shared.BackupStore) (*RemovalPlan, error) {
	content, exists, err := shared.ReadRegularFile(path)
	if err != nil || !exists {
		return nil, err
	}
	var config map[string]any
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	if err := decoder.Decode(&config); err != nil || config == nil {
		return nil, fmt.Errorf("Docker daemon.json 不是有效 JSON 对象，未移除日志轮转")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("Docker daemon.json 包含多余内容，未移除日志轮转")
	}
	status, configured := dockerLogConfigStatus(config)
	if !configured {
		return nil, nil
	}
	managed, err := store.MatchesManaged(path, content)
	if err != nil {
		return nil, err
	}
	if !managed {
		return nil, fmt.Errorf("Docker 日志配置没有本工具写入记录或已被修改，已保留，请手动检查: %s", path)
	}
	delete(config, "log-driver")
	delete(config, "log-opts")
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, err
	}
	return &RemovalPlan{path: path, original: content, cleaned: append(data, '\n'), status: status, store: store}, nil
}

func (plan *RemovalPlan) checkUnchanged() error {
	content, exists, err := shared.ReadRegularFile(plan.path)
	if err != nil {
		return err
	}
	if !exists || !bytes.Equal(content, plan.original) {
		return fmt.Errorf("Docker 日志配置在操作期间发生变化，未移除，请重试: %s", plan.path)
	}
	return nil
}

func (plan *RemovalPlan) Apply() error {
	if err := plan.checkUnchanged(); err != nil {
		return err
	}
	if err := shared.AtomicWriteFile(plan.path, plan.cleaned, shared.AtomicWriteOptions{Mode: 0644}); err != nil {
		return err
	}
	if err := plan.store.ForgetManaged(plan.path); err != nil {
		return fmt.Errorf("Docker 日志轮转已移除，但移除写入记录失败: %w", err)
	}
	return nil
}

func Remove(view *ui.UI) error {
	return remove(view, dockerDaemonPath, system.IsRoot, system.SystemdUnitExists, system.Run)
}

func remove(view removalConfirmer, path string, isRoot func() bool, unitExists func(string) bool, run systemRunFunc) error {
	ui.ClearScreen()
	ui.MenuTitle("容器管理", "Docker 配置", "移除日志轮转")
	if !isRoot() {
		return fmt.Errorf("移除 Docker 日志轮转需要 root 权限，请使用 sudo 运行本工具")
	}
	plan, err := PrepareRemoval(path, backupStore)
	if err != nil {
		return err
	}
	if plan == nil {
		ui.PrintSectionCard("无需移除",
			ui.CardField{Label: "配置文件", Value: path},
			ui.CardField{Label: "配置状态", Value: "未发现日志轮转设置"},
		)
		return nil
	}
	hasService := unitExists("docker.service")
	fields := []ui.CardField{{Label: "配置文件", Value: path}}
	fields = append(fields, logStatusFields(plan.status)...)
	fields = append(fields,
		ui.CardField{Label: "操作范围", Value: "移除 log-driver 和 log-opts，使用 Docker 默认日志设置"},
		ui.CardField{Label: "生效范围", Value: "后续新建容器；已有容器需重建"},
	)
	if hasService {
		fields = append(fields, ui.CardField{Label: "应用方式", Value: ui.DangerText("尝试重启 Docker，可能影响运行中容器")})
	}
	ui.PrintSectionCard("移除确认", fields...)
	fmt.Println()
	ok, err := view.Confirm("确认移除 Docker 日志轮转配置？(y/N)：")
	if err != nil {
		return err
	}
	if !ok {
		return shared.ErrReturnToMenu
	}
	if err := plan.checkUnchanged(); err != nil {
		return err
	}
	backup, err := backupStore.Snapshot(path)
	if err != nil {
		return fmt.Errorf("备份 Docker 日志配置失败，未移除: %w", err)
	}
	if err := plan.Apply(); err != nil {
		return err
	}
	if hasService {
		if err := run("systemctl", "try-restart", "docker.service"); err != nil {
			return fmt.Errorf("Docker 日志轮转已移除，但应用配置失败: %w", err)
		}
	}
	ui.PrintSuccessCard("Docker 日志轮转已移除",
		ui.CardField{Label: "配置文件", Value: path},
		ui.CardField{Label: "备份文件", Value: backup},
		ui.CardField{Label: "生效范围", Value: "后续新建容器；已有容器需重建"},
	)
	return nil
}
