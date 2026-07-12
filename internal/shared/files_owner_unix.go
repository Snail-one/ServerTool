//go:build unix

package shared

import (
	"fmt"
	"os"
	"syscall"
)

func fileOwner(info os.FileInfo) (*FileOwner, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, fmt.Errorf("无法读取 Unix 文件所有者")
	}
	return &FileOwner{UID: int(stat.Uid), GID: int(stat.Gid)}, nil
}
