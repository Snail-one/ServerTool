package golang

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"snail_tool/internal/ui"
)

const commandBinDir = "/usr/local/bin"

type commandLink struct {
	path   string
	target string
}

func goCommandLinks(binDir, current string) []commandLink {
	return []commandLink{
		{path: filepath.Join(binDir, "go"), target: filepath.Join(current, "bin", "go")},
		{path: filepath.Join(binDir, "gofmt"), target: filepath.Join(current, "bin", "gofmt")},
	}
}

func createGoCommandLinks() error {
	if err := ensureGoCommandLinks(commandBinDir, currentLink); err != nil {
		return err
	}
	ui.PrintSuccessCard("Go 命令软链接已就绪",
		ui.CardField{Label: "go", Value: filepath.Join(commandBinDir, "go") + " → " + filepath.Join(currentLink, "bin", "go")},
		ui.CardField{Label: "gofmt", Value: filepath.Join(commandBinDir, "gofmt") + " → " + filepath.Join(currentLink, "bin", "gofmt")},
	)
	return nil
}

func ensureGoCommandLinks(binDir, current string) error {
	if activeVersion(current) == "" {
		return errors.New("当前没有可用的工具管理 Go 版本，请先安装或切换 Go")
	}
	var missing []commandLink
	for _, link := range goCommandLinks(binDir, current) {
		info, err := os.Stat(link.target)
		if err != nil {
			return fmt.Errorf("检查 Go 命令 %s 失败: %w", link.target, err)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			return fmt.Errorf("%s 不是可执行文件，请先修复当前 Go", link.target)
		}
		matches, err := commandLinkMatches(link)
		if err != nil {
			return err
		}
		if matches {
			continue
		}
		if _, err := os.Lstat(link.path); err == nil {
			return fmt.Errorf("%s 已存在且不指向 %s，拒绝覆盖", link.path, link.target)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("检查命令路径 %s 失败: %w", link.path, err)
		}
		missing = append(missing, link)
	}
	if len(missing) == 0 {
		return nil
	}
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return fmt.Errorf("创建命令目录 %s 失败: %w", binDir, err)
	}
	var created []commandLink
	for _, link := range missing {
		if err := os.Symlink(link.target, link.path); err != nil {
			result := fmt.Errorf("创建软链接 %s 失败: %w", link.path, err)
			for _, previous := range created {
				result = errors.Join(result, removeGoCommandLink(previous))
			}
			return result
		}
		created = append(created, link)
	}
	return nil
}

func commandLinkMatches(link commandLink) (bool, error) {
	info, err := os.Lstat(link.path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("检查命令路径 %s 失败: %w", link.path, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return false, nil
	}
	target, err := os.Readlink(link.path)
	if err != nil {
		return false, fmt.Errorf("读取软链接 %s 失败: %w", link.path, err)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(link.path), target)
	}
	return filepath.Clean(target) == filepath.Clean(link.target), nil
}

func removeGoCommandLink(link commandLink) error {
	matches, err := commandLinkMatches(link)
	if err != nil || !matches {
		return err
	}
	if err := os.Remove(link.path); err != nil {
		return fmt.Errorf("清理 Go 命令软链接 %s 失败: %w", link.path, err)
	}
	return nil
}

func cleanupGoCommandLinks(binDir, current string) error {
	for _, link := range goCommandLinks(binDir, current) {
		if err := removeGoCommandLink(link); err != nil {
			return err
		}
	}
	return nil
}
