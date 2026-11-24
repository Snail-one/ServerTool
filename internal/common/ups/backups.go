package ups

import (
	"fmt"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

func backupNUTConfigFile(path string) (string, bool, error) {
	return backupStore.KeepOriginal(path)
}

func originalNUTBackupPath(path string) string {
	path, _ = backupStore.OriginalPath(path)
	return path
}

func restoreUPSBackup(view *ui.UI) error {
	backups := existingNUTBackups()
	if len(backups) == 0 {
		fmt.Println("未发现 UPS 配置备份文件")
		return nil
	}

	fmt.Println("将恢复以下备份文件：")
	for _, file := range backups {
		fmt.Printf("- %s -> %s\n", originalNUTBackupPath(file.path), file.path)
	}
	fmt.Println()

	confirmed, err := view.Confirm("确认恢复首次 UPS 配置备份？(y/N)：")
	if err != nil {
		return err
	}
	if !confirmed {
		fmt.Println("已取消恢复")
		return shared.ErrReturnToMenu
	}

	for _, file := range backups {
		if err := restoreNUTBackupFile(file); err != nil {
			return err
		}
	}
	warnIfNUTConfigPermissionsMismatch()

	if err := activateNUTServices(); err != nil {
		return err
	}

	fmt.Println()
	ui.PrintSuccessCard("UPS 首次配置备份已恢复",
		ui.CardField{Label: "恢复文件", Value: fmt.Sprintf("%d 个", len(backups))},
	)
	return nil
}

func deleteUPSBackups(view *ui.UI) error {
	all, err := backupStore.List()
	if err != nil {
		return err
	}
	allowed := make(map[string]bool)
	for _, file := range nutConfigFiles {
		allowed[originalNUTBackupPath(file.path)] = true
	}
	var backups []shared.BackupEntry
	for _, entry := range all {
		if allowed[entry.Path] {
			backups = append(backups, entry)
		}
	}
	if len(backups) == 0 {
		fmt.Println("未发现 UPS 配置备份文件")
		return nil
	}

	fmt.Println("将删除以下 UPS 配置备份文件：")
	for _, entry := range backups {
		fmt.Printf("- %s\n", entry.Path)
	}
	fmt.Println()

	confirmed, err := view.Confirm("确认删除 UPS 配置备份？(y/N)：")
	if err != nil {
		return err
	}
	if !confirmed {
		fmt.Println("已取消删除")
		return shared.ErrReturnToMenu
	}

	if _, err := backupStore.Delete(backups); err != nil {
		return err
	}

	fmt.Println()
	ui.PrintSuccessCard("UPS 配置备份已删除",
		ui.CardField{Label: "删除文件", Value: fmt.Sprintf("%d 个", len(backups))},
	)
	return nil
}

func existingNUTBackups() []nutConfigFile {
	backups := make([]nutConfigFile, 0, len(nutConfigFiles))
	for _, file := range nutConfigFiles {
		if system.FileExists(originalNUTBackupPath(file.path)) {
			backups = append(backups, file)
		}
	}
	return backups
}

func restoreNUTBackupFile(file nutConfigFile) error {
	backup, err := backupStore.ReadOriginal(file.path)
	if err != nil {
		return err
	}

	restoredFile := file
	restoredFile.mode = backup.Mode

	options := shared.AtomicWriteOptions{Mode: restoredFile.mode, ForceMode: true, Owner: backup.Owner}
	if shouldEnforceFileMetadata(restoredFile) {
		options.ForceMode = true
		uid, gid, ownerErr := lookupFileOwner(restoredFile.owner)
		if ownerErr != nil {
			return ownerErr
		}
		options.Owner = &shared.FileOwner{UID: uid, GID: gid}
	}
	if err := shared.AtomicWriteFile(restoredFile.path, backup.Data, options); err != nil {
		return err
	}
	if err := applyManagedFileMetadataIfNeeded(restoredFile); err != nil {
		return err
	}
	log.Info("已恢复 NUT 配置：", file.path)
	return nil
}
