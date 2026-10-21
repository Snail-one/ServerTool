package shared

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DataDir holds persistent data managed by this tool. System service
// configuration files remain in their original locations.
const DataDir = "/var/lib/snailtool"

// EnsurePrivateDir creates a directory with private permissions and refuses
// symlinks anywhere in its path. Existing ancestor permissions are preserved.
func EnsurePrivateDir(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	current := filepath.VolumeName(abs) + string(os.PathSeparator)
	if abs == current {
		return fmt.Errorf("拒绝将文件系统根目录用作私有数据目录")
	}
	for _, part := range strings.Split(strings.TrimPrefix(abs, current), string(os.PathSeparator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if err := os.Mkdir(current, 0700); err != nil && !os.IsExist(err) {
				return fmt.Errorf("创建数据目录失败 %s: %w", current, err)
			}
			info, err = os.Lstat(current)
		}
		if err != nil {
			return fmt.Errorf("检查数据目录失败 %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("数据目录路径不是普通目录，拒绝使用: %s", current)
		}
	}
	if err := os.Chmod(abs, 0700); err != nil {
		return fmt.Errorf("设置数据目录权限失败 %s: %w", abs, err)
	}
	return nil
}
