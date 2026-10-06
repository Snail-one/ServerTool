package packages

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Inventory shares one package database snapshot between the tool count,
// menu, and installation actions. Refresh it after an installation or on demand.
type Inventory struct {
	manager   packageManager
	installed map[string]bool
	err       error
}

func NewInventory() *Inventory {
	manager, err := detectPackageManager()
	inventory := &Inventory{manager: manager, err: err}
	if err == nil {
		_ = inventory.Refresh()
	}
	return inventory
}

func (inventory *Inventory) Refresh() error {
	if inventory.manager.name == "" {
		return inventory.err
	}
	inventory.installed, inventory.err = queryInstalledPackages(inventory.manager)
	return inventory.err
}

func (inventory *Inventory) InstalledCount() (int, int, error) {
	if inventory.err != nil {
		return 0, len(commonTools), inventory.err
	}
	installed := 0
	for _, tool := range commonTools {
		if inventory.toolInstalled(tool) {
			installed++
		}
	}
	return installed, len(commonTools), nil
}

func (inventory *Inventory) toolInstalled(tool commandLineTool) bool {
	if inventory.err != nil {
		return false
	}
	for _, name := range tool.packageNames(inventory.manager) {
		if !inventory.installed[name] {
			return false
		}
	}
	return true
}

// Reading the complete local database avoids nonzero "package not found"
// results from queries for individual missing packages. Any command failure
// is therefore a detection error, never a list of missing tools.
func queryInstalledPackages(manager packageManager) (map[string]bool, error) {
	var name string
	var args []string
	debian := false
	switch manager.name {
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
		return nil, fmt.Errorf("不支持查询 %s 的软件包状态", manager.name)
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

func packageQueryOutput(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
