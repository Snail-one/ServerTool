package backups

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

var dataDir = shared.DataDir

type module struct {
	key, name, directory string
}

var modules = []module{
	{"1", "防火墙", "firewall"},
	{"2", "SSH", "ssh"},
	{"3", "Docker", "docker"},
	{"4", "UPS", "ups"},
}

type inventory struct {
	module  module
	store   shared.BackupStore
	entries []shared.BackupEntry
}

type Plan struct {
	items []inventory
}

func PrepareAll() (*Plan, error) { return prepare(modules) }

func prepare(selected []module) (*Plan, error) {
	plan := &Plan{}
	for _, item := range selected {
		store := shared.BackupStore{Root: dataDir, Module: item.directory}
		entries, err := store.List()
		if err != nil {
			return nil, fmt.Errorf("读取 %s 备份失败: %w", item.name, err)
		}
		plan.items = append(plan.items, inventory{module: item, store: store, entries: entries})
	}
	return plan, nil
}

func (plan *Plan) Summary() string {
	count, size := plan.totals()
	return fmt.Sprintf("%d 个文件 · %s", count, formatSize(size))
}

func (plan *Plan) totals() (int, int64) {
	count, size := 0, int64(0)
	for _, item := range plan.items {
		count += len(item.entries)
		for _, entry := range item.entries {
			size += entry.Size()
		}
	}
	return count, size
}

func (plan *Plan) Delete() error {
	// Check every module before the first removal to avoid partial deletion
	// when a later module changed during confirmation.
	for _, item := range plan.items {
		if err := item.store.CheckEntries(item.entries); err != nil {
			return err
		}
	}
	deleted := 0
	for _, item := range plan.items {
		count, err := item.store.Delete(item.entries)
		deleted += count
		if err != nil {
			return fmt.Errorf("已清理 %d 个备份，后续清理停止: %w", deleted, err)
		}
	}
	ui.PrintSuccessCard("备份清理完成", ui.CardField{Label: "已删除", Value: fmt.Sprintf("%d 个备份文件", deleted)})
	return nil
}

func (plan *Plan) confirm(view *ui.UI) error {
	count, _ := plan.totals()
	if count == 0 {
		log.Info("当前没有可清理的备份")
		return nil
	}
	fields := []ui.CardField{{Label: "备份目录", Value: filepath.Join(dataDir, "backups")}}
	for _, item := range plan.items {
		fields = append(fields, ui.CardField{Label: item.module.name, Value: fmt.Sprintf("%d 个备份文件", len(item.entries))})
	}
	fields = append(fields,
		ui.CardField{Label: "清理合计", Value: plan.Summary()},
		ui.CardField{Label: "删除影响", Value: "所选备份将永久删除，无法再通过它们恢复配置"},
	)
	for _, item := range plan.items {
		if item.module.directory == "ups" && len(item.entries) > 0 {
			fields = append(fields, ui.CardField{Label: "UPS", Value: "将删除首次备份，之后无法通过它恢复初始配置"})
		}
	}
	ui.PrintDangerCard("确认清理备份", fields...)
	confirmed, err := view.Confirm("确认永久删除以上备份？(y/N)：")
	if err != nil {
		if err == io.EOF {
			log.Info("未收到确认，已保留备份")
			return nil
		}
		return err
	}
	if !confirmed {
		log.Info("已取消清理备份")
		return nil
	}
	return plan.Delete()
}

// ConfirmAll is shared by the command-line uninstall flow and the menu.
func ConfirmAll(view *ui.UI) error {
	plan, err := PrepareAll()
	if err != nil {
		return err
	}
	return plan.confirm(view)
}

// DeleteAll is used when uninstalling: uninstall explicitly includes backups.
func DeleteAll() error {
	plan, err := PrepareAll()
	if err != nil {
		return err
	}
	ui.PrintInfoCard("清理全部备份", ui.CardField{Label: "清理合计", Value: plan.Summary()})
	return plan.Delete()
}

func Run(view *ui.UI) error {
	for {
		ui.ClearScreen()
		ui.MenuTitle("清理配置", "清理备份")
		plan, err := PrepareAll()
		if err != nil {
			return err
		}
		ui.PrintInfoCard("备份概况", ui.CardField{Label: "备份合计", Value: plan.Summary()})
		for _, item := range plan.items {
			ui.MenuOptionHint(item.module.key, "清理 "+item.module.name+" 备份", fmt.Sprintf("%d 个文件", len(item.entries)))
		}
		ui.MenuOption("5", "清理全部备份")
		ui.MenuExit("0/q", "返回")
		choice, err := view.Ask("请选择：")
		if err != nil {
			return err
		}
		if shared.IsReturnChoice(choice) {
			return shared.ErrReturnToMenu
		}
		var selected []module
		if choice == "5" {
			selected = modules
		} else {
			for _, item := range modules {
				if item.key == strings.ToLower(choice) {
					selected = []module{item}
				}
			}
		}
		if len(selected) == 0 {
			ui.InvalidChoice()
			view.Pause()
			continue
		}
		shared.RunAction(view, "备份清理失败，已返回备份管理", func() error {
			chosen, err := prepare(selected)
			if err != nil {
				return err
			}
			return chosen.confirm(view)
		})
	}
}

func formatSize(size int64) string {
	value := float64(size)
	for _, unit := range []string{"B", "KiB", "MiB", "GiB", "TiB"} {
		if value < 1024 || unit == "TiB" {
			return fmt.Sprintf("%.1f %s", value, unit)
		}
		value /= 1024
	}
	return ""
}
