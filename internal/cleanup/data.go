package cleanup

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

// DeleteSoftwareData is used after consent during uninstall or full cleanup.
// Keep its target fixed; callers cannot supply an arbitrary directory to delete.
func DeleteSoftwareData() error {
	info, err := inspectSoftwareData(shared.DataDir)
	if err != nil {
		return err
	}
	if info == nil {
		ui.PrintInfoCard("软件数据目录不存在", ui.CardField{Label: "目录", Value: shared.DataDir})
		return nil
	}
	return deleteSoftwareData(shared.DataDir, info)
}

// The menu always supplies shared.DataDir; the argument isolates filesystem tests.
func cleanupSoftwareData(view *ui.UI, root string) error {
	info, err := inspectSoftwareData(root)
	if err != nil {
		return err
	}
	ui.ClearScreen()
	ui.MenuTitle("清理配置", "删除软件数据目录")
	if info == nil {
		ui.PrintInfoCard("软件数据目录不存在", ui.CardField{Label: "目录", Value: root})
		return nil
	}
	ui.PrintDangerCard("删除整个软件数据目录",
		ui.CardField{Label: "目标目录", Value: root},
		ui.CardField{Label: "删除内容", Value: "整个目录及全部子目录、备份、首次快照、锁文件、隐藏文件和其他文件"},
		ui.CardField{Label: "恢复资料", Value: "永久删除，之后无法通过这些备份恢复配置"},
		ui.CardField{Label: "系统配置", Value: "现有配置和服务保持生效，此操作只删除上述数据目录"},
	)
	confirmed, err := view.Confirm("确认删除整个软件数据目录？(y/N)：")
	if err != nil {
		return err
	}
	if !confirmed {
		log.Info("已取消删除软件数据目录")
		return shared.ErrReturnToMenu
	}
	return deleteSoftwareData(root, info)
}

func deleteSoftwareData(root string, expected os.FileInfo) error {
	latest, err := inspectSoftwareData(root)
	if err != nil {
		return err
	}
	if latest == nil || expected == nil || !os.SameFile(expected, latest) {
		return fmt.Errorf("软件数据目录在确认期间发生变化，未删除，请重新操作")
	}
	unlock, err := lockSoftwareData(root)
	if err != nil {
		return err
	}
	defer unlock()
	latest, err = inspectSoftwareData(root)
	if err != nil {
		return err
	}
	if latest == nil || !os.SameFile(expected, latest) {
		return fmt.Errorf("软件数据目录在清理准备期间发生变化，未删除，请重新操作")
	}
	if err := shared.RemovePathTree(root); err != nil {
		return fmt.Errorf("删除软件数据目录未完成，请检查剩余内容: %w", err)
	}
	ui.PrintSuccessCard("软件数据目录已删除", ui.CardField{Label: "已删除", Value: root})
	return nil
}

func inspectSoftwareData(root string) (os.FileInfo, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || filepath.Base(root) != "snailtool" {
		return nil, fmt.Errorf("软件数据目录路径无效: %s", root)
	}
	var target os.FileInfo
	for path := root; ; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) && path == root {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("软件数据目录或父级不是普通目录，拒绝删除: %s", path)
		}
		if path == root {
			target = info
		}
		if filepath.Dir(path) == path {
			return target, nil
		}
	}
}

// Hold existing backup locks until deletion finishes. Removing a locked inode
// while another backup is using it would break coordination between writers.
func lockSoftwareData(root string) (func(), error) {
	var locks []*os.File
	unlock := func() {
		for _, file := range locks {
			_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
			_ = file.Close()
		}
	}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		// WalkDir does not follow directory symlinks. RemoveAll removes their
		// entries without touching their targets outside the data directory.
		if entry.Name() != ".snailtool-backup.lock" || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		file, err := os.OpenFile(path, os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			return err
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			_ = file.Close()
			return fmt.Errorf("备份锁文件类型异常，未删除数据目录: %s", path)
		}
		if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			_ = file.Close()
			return fmt.Errorf("备份目录正在使用或无法锁定，未删除软件数据目录: %w", err)
		}
		locks = append(locks, file)
		return nil
	})
	if err != nil {
		unlock()
		return nil, err
	}
	return unlock, nil
}
