package golang

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"snail_tool/internal/system"
)

const (
	installRoot = "/opt/go"
	currentLink = "/opt/go/current"
	managedFile = ".servertool-managed"
)

// CurrentVersion returns the Go version selected by ServerTool.
func CurrentVersion() string {
	return activeVersion(currentLink)
}

func versionInstallPath(version string) string {
	return filepath.Join(installRoot, version)
}

func installedVersions(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取 Go 安装目录失败: %w", err)
	}
	versions := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() && validVersion(entry.Name()) && isManagedVersion(filepath.Join(root, entry.Name())) {
			versions = append(versions, entry.Name())
		}
	}
	sort.Slice(versions, func(i, j int) bool { return compareVersions(versions[i], versions[j]) > 0 })
	return versions, nil
}

func activeVersion(link string) string {
	target, err := os.Readlink(link)
	if err != nil {
		return ""
	}
	version := filepath.Base(filepath.Clean(target))
	if !validVersion(version) || !isManagedVersion(link) {
		return ""
	}
	return version
}

func activateVersion(root, link, version string) error {
	if !validVersion(version) || !isManagedVersion(filepath.Join(root, version)) {
		return fmt.Errorf("Go 版本目录无效：%s", version)
	}
	if info, err := os.Lstat(link); err == nil && info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("%s 已存在且不是软链接，拒绝覆盖", link)
	} else if err == nil && !managedLinkPath(root, link) {
		return fmt.Errorf("%s 指向非托管路径，拒绝覆盖", link)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	temporary := filepath.Join(filepath.Dir(link), ".current.tmp")
	_ = os.Remove(temporary)
	if err := os.Symlink(filepath.Join(root, version), temporary); err != nil {
		return err
	}
	if err := os.Rename(temporary, link); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func managedLinkPath(root, link string) bool {
	target, err := os.Readlink(link)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(link), target)
	}
	target = filepath.Clean(target)
	return filepath.Dir(target) == filepath.Clean(root) && validVersion(filepath.Base(target))
}

func isManagedVersion(path string) bool {
	return system.FileExists(filepath.Join(path, "bin", "go")) &&
		system.FileExists(filepath.Join(path, managedFile))
}

func removeCurrentLink(link string) error {
	info, err := os.Lstat(link)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("%s 不是本工具可安全删除的软链接", link)
	}
	return os.Remove(link)
}

func installArtifacts(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取 Go 安装目录失败: %w", err)
	}
	artifacts := make([]string, 0)
	for _, entry := range entries {
		name := entry.Name()
		if name == ".current.tmp" ||
			strings.HasPrefix(name, ".backup-") ||
			strings.HasPrefix(name, ".repair-") ||
			strings.HasPrefix(name, ".install-") ||
			(strings.HasPrefix(name, ".download-") && strings.HasSuffix(name, ".tar.gz")) {
			artifacts = append(artifacts, name)
		}
	}
	sort.Strings(artifacts)
	return artifacts, nil
}
