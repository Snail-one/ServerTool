package vscodeserver

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type remoteProcesses struct {
	uid   int
	roots []string
}

func (p remoteProcesses) list() ([]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("无法读取进程列表: %w", err)
	}
	var pids []int
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 || pid == os.Getpid() {
			continue
		}
		matches, err := p.matches(pid)
		if err != nil {
			return nil, fmt.Errorf("无法检查进程 %d: %w", pid, err)
		}
		if matches {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}

func (p remoteProcesses) matches(pid int) (bool, error) {
	dir := filepath.Join("/proc", strconv.Itoa(pid))
	status, err := os.ReadFile(filepath.Join(dir, "status"))
	if err != nil {
		return false, ignoreExited(err)
	}
	uid := strconv.Itoa(p.uid)
	ids := processStatusField(status, "Uid:")
	if len(ids) < 2 || ids[0] != uid || ids[1] != uid {
		return false, nil
	}
	executable, err := os.Readlink(filepath.Join(dir, "exe"))
	if err != nil {
		// An executable may be hidden by Linux process permissions even for the
		// same UID. The command line can still identify its installation.
		if os.IsPermission(err) {
			args, err := os.ReadFile(filepath.Join(dir, "cmdline"))
			if err != nil {
				return false, ignoreExited(err)
			}
			command, _, _ := strings.Cut(string(args), "\x00")
			return p.inInstallation(command), nil
		}
		return false, ignoreExited(err)
	}
	if p.inInstallation(strings.TrimSuffix(executable, " (deleted)")) {
		return true, nil
	}

	// The server's shell entry point uses a system interpreter. Verify the
	// script argument so a path mentioned by an unrelated command is ignored.
	switch filepath.Base(executable) {
	case "sh", "bash", "dash", "ash", "busybox":
		args, err := os.ReadFile(filepath.Join(dir, "cmdline"))
		if err != nil {
			return false, ignoreExited(err)
		}
		argv := strings.SplitN(string(args), "\x00", 3)
		if len(argv) >= 2 && p.inInstallation(argv[1]) {
			return strings.HasSuffix(argv[1], "/bin/code-server") || strings.HasSuffix(argv[1], "/server.sh"), nil
		}
	}
	return false, nil
}

func (p remoteProcesses) inInstallation(path string) bool {
	path = filepath.Clean(path)
	for _, root := range p.roots {
		if strings.HasPrefix(path, root+"/") {
			return true
		}
	}
	return false
}

func (p remoteProcesses) stop() error {
	pids, err := p.list()
	if err != nil || len(pids) == 0 {
		return err
	}
	ancestors, err := ancestorPIDs()
	if err != nil {
		return err
	}
	for _, signal := range []os.Signal{syscall.SIGTERM, os.Kill} {
		for _, pid := range pids {
			if ancestors[pid] {
				return fmt.Errorf("当前终端由 VS Code Server 提供，请通过普通 SSH 或本机终端重新运行")
			}
		}
		for _, pid := range pids {
			if err := p.signal(pid, signal); err != nil {
				return fmt.Errorf("停止 VS Code Server 进程 %d 失败: %w", pid, err)
			}
		}
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			pids, err = p.list()
			if err != nil || len(pids) == 0 {
				return err
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	return fmt.Errorf("VS Code Server 进程仍在运行（PID %v），未删除目录", pids)
}

func (p remoteProcesses) signal(pid int, signal os.Signal) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return ignoreExited(err)
	}
	defer process.Release()
	matches, err := p.matches(pid)
	if err != nil || !matches {
		return err
	}
	return ignoreExited(process.Signal(signal))
}

func ancestorPIDs() (map[int]bool, error) {
	ancestors := make(map[int]bool)
	for pid := os.Getppid(); pid > 1 && !ancestors[pid]; {
		ancestors[pid] = true
		status, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "status"))
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("无法检查当前终端的父进程: %w", err)
		}
		parent := processStatusField(status, "PPid:")
		if len(parent) != 1 {
			return nil, fmt.Errorf("无法识别进程 %d 的父进程", pid)
		}
		pid, err = strconv.Atoi(parent[0])
		if err != nil {
			return nil, fmt.Errorf("无法识别进程 %d 的父进程: %w", pid, err)
		}
	}
	return ancestors, nil
}

func processStatusField(status []byte, name string) []string {
	for line := range strings.SplitSeq(string(status), "\n") {
		if value, found := strings.CutPrefix(line, name); found {
			return strings.Fields(value)
		}
	}
	return nil
}

func ignoreExited(err error) error {
	if os.IsNotExist(err) || errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
