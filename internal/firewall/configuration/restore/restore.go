package restore

import (
	"fmt"
	"reflect"
	"strings"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/firewall/forward/dockerbridge"
	rulespkg "snail_tool/internal/firewall/internal/rules"
	"snail_tool/internal/firewall/loadedrules"
	"snail_tool/internal/firewall/portmapping"
	autostart "snail_tool/internal/firewall/settings/autostart"
	"snail_tool/internal/firewall/settings/ping"
	"snail_tool/internal/firewall/settings/sshaccess"
	"snail_tool/internal/log"
	"snail_tool/internal/storage"
	"snail_tool/internal/ui"
)

type Service struct {
	Store            configuration.Store
	IsRoot           func() bool
	CommandExists    func(string) bool
	HasManagedConfig func(string) bool
	ReadOriginal     func() (*configuration.OriginalState, error)
	Autostart        autostart.Service
}

type Plan struct {
	service       Service
	version       configuration.Version
	original      *configuration.OriginalState
	cleaned       []byte
	managed       bool
	loaded        bool
	boot          *autostart.Status
	target        string
	backupCurrent bool
}

func (service Service) Prepare() (*Plan, error) {
	original, err := service.ReadOriginal()
	if err != nil {
		return nil, err
	}
	version, err := service.Store.Read()
	if err != nil {
		return nil, err
	}
	plan := &Plan{service: service, version: version, original: original}
	content := string(version.Content)
	plan.managed = service.HasManagedConfig(content)
	blocks := [][2]string{
		{configuration.ManagedBegin, configuration.ManagedEnd},
		{rulespkg.AccessBegin, rulespkg.AccessEnd},
		{sshaccess.LegacyBegin, sshaccess.LegacyEnd},
		{ping.Begin, ping.End},
		{dockerbridge.Begin, dockerbridge.End},
		{portmapping.Begin, portmapping.End},
	}
	// Validate all nested markers before removing the outer configuration block.
	for _, markers := range blocks {
		begins, ends := strings.Count(content, markers[0]), strings.Count(content, markers[1])
		if begins == 0 && ends == 0 {
			continue
		}
		if begins != 1 || ends != 1 || strings.Index(content, markers[0]) > strings.Index(content, markers[1]) {
			return nil, fmt.Errorf("防火墙托管标记损坏，未清理配置或规则")
		}
	}
	if service.CommandExists("nft") {
		live := loadedrules.Inspect(service.Store.CommandOutput)
		if live.Err != nil {
			return nil, fmt.Errorf("检查防火墙规则失败，未清理: %w", live.Err)
		}
		plan.loaded = live.SnailLoaded
	} else if plan.managed || original != nil {
		return nil, fmt.Errorf("未找到 nft，无法确认或清理已加载规则，已保留配置和首次快照")
	}
	if plan.loaded && !plan.managed && original == nil {
		return nil, fmt.Errorf("inet snail 表缺少本工具托管配置及首次快照，无法确认归属，未删除")
	}
	cleaned := content
	for _, markers := range blocks {
		cleaned, err = storage.RemoveManagedBlockChecked(cleaned, markers[0], markers[1])
		if err != nil {
			return nil, err
		}
	}
	plan.cleaned = []byte(storage.NormalizeCleanedContent(cleaned))
	if original != nil {
		plan.target = original.Autostart
	} else if plan.managed {
		tokens, err := configuration.Tokens(string(plan.cleaned))
		if err != nil {
			return nil, err
		}
		if len(tokens) == 0 {
			plan.target = "disabled"
		}
	}
	if plan.target != "" && service.CommandExists("systemctl") {
		status := service.Autostart.Inspect()
		if status.Err != nil {
			if original == nil && status.Label == "服务不可用" {
				plan.target = ""
				return plan, nil
			}
			return nil, status.Err
		}
		if !configuration.ValidOriginalAutostart(status.State) {
			return nil, fmt.Errorf("无法恢复 nftables 自启状态 %q，未清理", status.State)
		}
		if original == nil && !service.Autostart.LoadsConfig(status.ExecStart) {
			plan.target = ""
		} else {
			plan.boot = &status
		}
	} else if plan.target != "" && original != nil {
		return nil, fmt.Errorf("未找到 systemctl，无法恢复首次记录的开机自启状态")
	}
	return plan, nil
}

func (service Service) Run() error {
	if !service.IsRoot() {
		return fmt.Errorf("清理防火墙需要 root 权限，请使用 sudo 运行本工具")
	}
	plan, err := service.Prepare()
	if err != nil {
		return err
	}
	return plan.Run()
}

// CleanupWithConfirm is the dedicated action under the configuration cleanup menu.
func (service Service) RunWithConfirm(view *ui.UI) error {
	if !service.IsRoot() {
		return fmt.Errorf("清理防火墙需要 root 权限，请使用 sudo 运行本工具")
	}
	plan, err := service.Prepare()
	if err != nil {
		return err
	}
	ui.ClearScreen()
	plan.backupCurrent = true
	ui.MenuTitle("清理配置", "防火墙配置")
	configScope := "移除本工具托管区块，保留其他文件内容"
	bootScope := "仅在清理后没有其他配置时关闭本文件的开机自启"
	if plan.original != nil {
		configScope = "恢复首次接入前的完整配置、权限和所有者；覆盖接入后的文件修改"
		if plan.original.Config == nil {
			configScope = "首次接入时配置文件不存在，将移除本工具创建的配置文件"
		}
		bootScope = "恢复首次接入前的开机自启状态"
	}
	ui.PrintWarningCard("确认防火墙清理范围",
		ui.CardField{Label: "配置文件", Value: configScope},
		ui.CardField{Label: "当前规则", Value: "删除 inet snail 表，立即解除本工具的入站限制"},
		ui.CardField{Label: "开机自启", Value: bootScope},
		ui.CardField{Label: "备份", Value: "清理前另存当前配置，保留首次快照和历史备份"},
	)
	if plan.original == nil {
		log.Warn("没有接入前的首次快照，只能移除可识别的本工具内容，无法完整恢复历史配置及自启状态。")
	}
	confirmed, err := view.Confirm("确认一键清理防火墙？(y/N，0/q 返回)：")
	if err != nil {
		return err
	}
	if !confirmed {
		log.Info("已取消防火墙清理")
		return ui.ErrReturnToMenu
	}
	if err := plan.Run(); err != nil {
		return err
	}
	result := "已移除可识别的本工具配置及规则；没有首次快照，历史状态无法完整恢复"
	if plan.original != nil {
		result = "已恢复首次接入前的配置和开机自启状态，并移除本工具规则"
	}
	ui.PrintSuccessCard("防火墙清理完成", ui.CardField{Label: "结果", Value: result})
	return nil
}

func (plan *Plan) Run() error {
	service := plan.service
	if err := service.Store.Check(plan.version); err != nil {
		return err
	}
	original, err := service.ReadOriginal()
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(original, plan.original) {
		return fmt.Errorf("首次快照在确认期间发生变化，未清理，请重新操作")
	}
	if plan.boot != nil {
		latest := service.Autostart.Inspect()
		if latest.Err != nil {
			return latest.Err
		}
		if latest.State != plan.boot.State || latest.ExecStart != plan.boot.ExecStart {
			return fmt.Errorf("nftables 服务在确认期间发生变化，未清理，请重新操作")
		}
	}
	// The general cleanup flow inventories backups before running its steps.
	// Only the firewall's own action creates a new pre-cleanup history entry.
	if plan.backupCurrent && plan.version.Exists && (plan.managed || plan.original != nil) {
		backup, err := service.Store.Backup()
		if err != nil {
			return err
		}
		log.Info("已备份清理前的防火墙配置：", backup)
	}
	if err := service.Store.Check(plan.version); err != nil {
		return err
	}
	if plan.boot != nil && plan.target != plan.boot.State {
		if err := service.Autostart.Restore(plan.target, plan.boot.State); err != nil {
			return fmt.Errorf("恢复开机自启失败，配置文件和当前规则已保留；备份已保留，可重试: %w", err)
		}
	}
	if plan.loaded {
		if output, err := service.Store.CommandOutput("nft", "delete", "table", "inet", "snail"); err != nil {
			return fmt.Errorf("删除 inet snail 表失败，当前限制可能仍生效；配置及备份已保留，自启设置可能已恢复，可重试: %w: %s", err, strings.TrimSpace(output))
		}
		live := loadedrules.Inspect(service.Store.CommandOutput)
		if live.Err != nil || live.SnailLoaded {
			return fmt.Errorf("规则删除后未能确认 inet snail 表已移除，请刷新检查；首次快照和备份已保留")
		}
	}
	var restoreErr error
	switch {
	case plan.original != nil && plan.original.Config != nil:
		c := plan.original.Config
		restoreErr = storage.AtomicWriteFile(service.Store.Path, c.Data, storage.AtomicWriteOptions{Mode: c.Mode, ForceMode: true, Owner: c.Owner})
	case plan.original != nil:
		restoreErr = storage.RemoveRegularFile(service.Store.Path)
	case plan.managed:
		restoreErr = storage.AtomicWriteFile(service.Store.Path, plan.cleaned, storage.AtomicWriteOptions{Mode: 0644})
	}
	if restoreErr != nil {
		return fmt.Errorf("本工具规则已关闭，自启设置已处理，但配置文件恢复失败；恢复资料已保留，可修正后重试: %w", restoreErr)
	}
	if plan.original == nil {
		log.Info("已移除可识别的防火墙托管配置与本工具规则，其他规则表保持原状")
	} else {
		log.Info("已恢复接入前的防火墙配置和开机自启，本工具规则已关闭，其他规则表保持原状")
	}
	return nil
}
