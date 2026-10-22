package backups

import (
	"fmt"
	"io"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

var legacyStore = shared.LegacyBackupStore{Root: "/"}

func cleanLegacy(view *ui.UI) error {
	entries, err := legacyStore.List()
	if err != nil {
		return fmt.Errorf("检测旧备份失败，未删除文件: %w", err)
	}
	if len(entries) == 0 {
		log.Info("未检测到旧位置的备份文件")
		return nil
	}
	ui.MenuTitle("清理配置", "清理旧备份")
	fmt.Println("检测到以下旧位置备份，请逐项核对：")
	fmt.Println()
	var total int64
	for index, entry := range entries {
		total += entry.Size()
		fmt.Printf("%d. %s\n   %s · 修改于 %s\n", index+1, entry.Path, formatSize(entry.Size()), entry.ModTime().Format("2006-01-02 15:04:05 MST"))
	}
	ui.PrintDangerCard("确认清理旧备份",
		ui.CardField{Label: "清理合计", Value: fmt.Sprintf("%d 个文件 · %s", len(entries), formatSize(total))},
		ui.CardField{Label: "检测依据", Value: "旧版本的路径和命名格式，可能包含其他工具生成的同名备份，请核对清单"},
		ui.CardField{Label: "删除影响", Value: "以上备份将永久删除，包含检测到的 UPS 首次备份"},
	)
	confirmed, err := view.Confirm("确认永久删除清单中的全部旧备份？(y/N)：")
	if err == io.EOF {
		log.Info("未收到确认，已保留旧备份")
		return nil
	}
	if err != nil {
		return err
	}
	if !confirmed {
		log.Info("已取消清理旧备份")
		return shared.ErrReturnToMenu
	}
	deleted, err := legacyStore.Delete(entries)
	if err != nil {
		return fmt.Errorf("已删除 %d 个旧备份，后续清理停止: %w", deleted, err)
	}
	ui.PrintSuccessCard("旧备份清理完成", ui.CardField{Label: "已删除", Value: fmt.Sprintf("%d 个旧备份文件", deleted)})
	return nil
}
