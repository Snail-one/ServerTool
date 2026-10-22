// Package tempguard coordinates temporary-file producers with interrupted-file cleanup.
package tempguard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Guard uses a stable lock file which must never be unlinked on release.
// Keeping the inode allows Go processes and the shell installer to share flock.
type Guard struct {
	Path string
}

func Default() Guard {
	// Do not use TMPDIR: installers and cleanup must agree even across environments.
	return Guard{Path: filepath.Join("/tmp", fmt.Sprintf(".servertool-temps-%d.lock", os.Geteuid()))}
}

// Use protects the entire lifetime of temporary files, including closed files
// needed by a later installation step. Multiple producers may run concurrently.
func Use() (func(), error) { return Default().Use() }

func (guard Guard) Use() (func(), error) { return guard.acquire(false) }

// Cleanup prevents producers from starting until scanning or deletion finishes.
func (guard Guard) Cleanup() (func(), error) { return guard.acquire(true) }

func (guard Guard) acquire(exclusive bool) (func(), error) {
	fd, err := syscall.Open(guard.Path, syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, fmt.Errorf("打开临时文件保护锁失败: %w", err)
	}
	file := os.NewFile(uintptr(fd), guard.Path)
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("检查临时文件保护锁失败: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 {
		_ = file.Close()
		return nil, fmt.Errorf("临时文件保护锁类型、权限或属主异常，已停止: %s", guard.Path)
	}
	operation := syscall.LOCK_SH
	if exclusive {
		operation = syscall.LOCK_EX
	}
	if err := syscall.Flock(fd, operation|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			if exclusive {
				return nil, fmt.Errorf("安装、更新、配置或其他临时文件清理正在进行，请稍后重试")
			}
			return nil, fmt.Errorf("正在清理中断临时文件，请稍后重试")
		}
		return nil, fmt.Errorf("获取临时文件保护锁失败: %w", err)
	}
	// Closing releases this descriptor's lock without affecting nested producers.
	return func() { _ = file.Close() }, nil
}
