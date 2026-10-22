package temps

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

type openUse struct {
	info os.FileInfo
	path string
}

// currentUses lists files and directories other file descriptors or working
// directories still reference. A matching name is not an interrupted leftover
// while a process is using it.
func currentUses() ([]openUse, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("无法确认临时文件是否仍在使用，已停止: %w", err)
	}
	uses := make([]openUse, 0)
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		base := filepath.Join("/proc", entry.Name())
		if use, ok := readUse(filepath.Join(base, "cwd")); ok {
			uses = append(uses, use)
		}
		fds, err := os.ReadDir(filepath.Join(base, "fd"))
		if err != nil {
			if ignorableProcErr(err) {
				continue
			}
			return nil, fmt.Errorf("无法确认临时文件是否仍在使用，已停止: %w", err)
		}
		for _, fd := range fds {
			if use, ok := readUse(filepath.Join(base, "fd", fd.Name())); ok {
				uses = append(uses, use)
			}
		}
	}
	return uses, nil
}

func readUse(procPath string) (openUse, bool) {
	info, err := os.Stat(procPath)
	if err != nil {
		return openUse{}, false
	}
	link, err := os.Readlink(procPath)
	if err != nil {
		return openUse{info: info}, true
	}
	link = strings.TrimSuffix(link, " (deleted)")
	if !filepath.IsAbs(link) {
		return openUse{info: info}, true
	}
	return openUse{info: info, path: filepath.Clean(link)}, true
}

func usesPath(uses []openUse, path string, info os.FileInfo) bool {
	if info == nil {
		return false
	}
	path = filepath.Clean(path)
	prefix := path + string(os.PathSeparator)
	for _, use := range uses {
		if use.info != nil && os.SameFile(info, use.info) {
			return true
		}
		if use.path == path || (info.IsDir() && use.path != "" && strings.HasPrefix(use.path, prefix)) {
			return true
		}
	}
	return false
}

func ignorableProcErr(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.ESRCH) || errors.Is(err, syscall.EINVAL)
}
