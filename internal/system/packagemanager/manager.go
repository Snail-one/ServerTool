package packagemanager

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"snail_tool/internal/log"
)

type Manager struct {
	Name        string
	RefreshArgs []string
	InstallArgs []string
}

func Detect(commandExists func(string) bool) (Manager, error) {
	for _, candidate := range []Manager{
		{Name: "apt-get", RefreshArgs: []string{"update"}, InstallArgs: []string{"install", "-y"}},
		{Name: "apt", RefreshArgs: []string{"update"}, InstallArgs: []string{"install", "-y"}},
		{Name: "dnf", InstallArgs: []string{"install", "-y"}},
		{Name: "yum", InstallArgs: []string{"install", "-y"}},
		{Name: "pacman", InstallArgs: []string{"-Sy", "--noconfirm", "--needed"}},
		{Name: "zypper", InstallArgs: []string{"--non-interactive", "install"}},
		{Name: "apk", InstallArgs: []string{"add"}},
	} {
		if commandExists(candidate.Name) {
			return candidate, nil
		}
	}
	return Manager{}, fmt.Errorf("未识别支持的包管理器，请手动安装所需工具")
}

func (manager Manager) Install(commandRun func(string, ...string) error, packages []string) error {
	if len(manager.RefreshArgs) > 0 {
		log.Info("更新软件包索引...")
		if err := commandRun(manager.Name, manager.RefreshArgs...); err != nil {
			return fmt.Errorf("%s 更新软件包索引失败: %w", manager.Name, err)
		}
	}
	args := append(append([]string{}, manager.InstallArgs...), packages...)
	log.Info("安装软件包：", strings.Join(packages, "、"))
	if err := commandRun(manager.Name, args...); err != nil {
		return fmt.Errorf("%s 安装软件包失败: %w", manager.Name, err)
	}
	return nil
}

// Reading the complete local database avoids nonzero "package not found"
// results from queries for individual missing packages. Any command failure
// is therefore a detection error, never a list of missing tools.
func Installed(manager Manager, commandOutput func(string, ...string) (string, error)) (map[string]bool, error) {
	var name string
	var args []string
	debian := false
	switch manager.Name {
	case "apt-get", "apt":
		name, args = "dpkg-query", []string{"-W", "-f=${Package}\t${Status}\n"}
		debian = true
	case "dnf", "yum", "zypper":
		name, args = "rpm", []string{"-qa", "--queryformat", "%{NAME}\n"}
	case "pacman":
		name, args = "pacman", []string{"-Qq"}
	case "apk":
		name, args = "apk", []string{"info"}
	default:
		return nil, fmt.Errorf("不支持查询 %s 的软件包状态", manager.Name)
	}
	output, err := commandOutput(name, args...)
	if err != nil {
		return nil, fmt.Errorf("%s 查询已安装软件包失败: %w", name, err)
	}
	installed := make(map[string]bool)
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		packageName := line
		if debian {
			var status string
			var ok bool
			packageName, status, ok = strings.Cut(line, "\t")
			fields := strings.Fields(status)
			if !ok || packageName == "" || len(fields) != 3 {
				return nil, fmt.Errorf("%s 返回的软件包记录格式异常", name)
			}
			if fields[1] != "ok" || fields[2] != "installed" {
				continue
			}
			packageName, _, _ = strings.Cut(packageName, ":")
		}
		if len(strings.Fields(packageName)) != 1 {
			return nil, fmt.Errorf("%s 返回的软件包名称格式异常", name)
		}
		installed[packageName] = true
	}
	return installed, nil
}

func QueryOutput(name string, args ...string) (string, error) {
	return QueryOutputContext(context.Background(), name, args...)
}

func QueryOutputContext(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = time.Second
	output, err := cmd.Output()
	if ctx.Err() != nil {
		return "", fmt.Errorf("查询超时: %w", ctx.Err())
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if detail := strings.TrimSpace(string(exitErr.Stderr)); detail != "" {
				return "", fmt.Errorf("%w: %s", err, detail)
			}
		}
		return "", err
	}
	return string(output), nil
}
