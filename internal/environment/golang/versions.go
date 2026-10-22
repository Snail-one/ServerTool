package golang

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/tempguard"
)

// errNotInstallArtifact marks a directory entry whose name is not an install leftover.
// A recognized name with the wrong type is a different error and must stop cleanup.
var errNotInstallArtifact = errors.New("不是本工具的 Go 安装残留")

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
	if !managedLinkPath(filepath.Dir(link), link) {
		return ""
	}
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
	release, err := tempguard.Use()
	if err != nil {
		return err
	}
	defer release()

	if err := shared.CheckRemovalTree(root); err != nil {
		return err
	}
	if !validVersion(version) || !isManagedDirectory(filepath.Join(root, version)) {
		return fmt.Errorf("Go 版本目录无效：%s", version)
	}
	if info, err := os.Lstat(link); err == nil && info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("%s 已存在且不是软链接，拒绝覆盖", link)
	} else if err == nil && !managedLinkPath(root, link) {
		return fmt.Errorf("%s 指向非托管路径，拒绝覆盖", link)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	temporary, err := unusedTempPath(filepath.Dir(link), ".current-*")
	if err != nil {
		return err
	}
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

func isManagedDirectory(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() {
		return false
	}
	for _, name := range []string{managedFile, filepath.Join("bin", "go")} {
		if _, exists, err := shared.ReadRegularFile(filepath.Join(path, name)); err != nil || !exists {
			return false
		}
	}
	return true
}

func removeCurrentLink(link string) error {
	info, err := os.Lstat(link)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink == 0 || !managedLinkPath(filepath.Dir(link), link) {
		return fmt.Errorf("%s 不是本工具可安全删除的软链接", link)
	}
	return os.Remove(link)
}

// InstallArtifact is one interrupted Go install file or directory under the install root.
type InstallArtifact struct {
	Root string
	Name string
	Info os.FileInfo
}

// ListInstallArtifacts returns leftovers that the Go installer would clean.
func ListInstallArtifacts(root string) ([]InstallArtifact, error) {
	names, err := installArtifacts(root)
	if err != nil {
		return nil, err
	}
	artifacts := make([]InstallArtifact, 0, len(names))
	for _, name := range names {
		info, err := installArtifactInfo(root, name)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, InstallArtifact{Root: root, Name: name, Info: info})
	}
	return artifacts, nil
}

// DeleteInstallArtifact removes one previously listed leftover after rechecking its identity.
func DeleteInstallArtifact(artifact InstallArtifact) error {
	return removeInstallArtifact(artifact.Root, artifact.Name, artifact.Info)
}

func installArtifacts(root string) ([]string, error) {
	if err := shared.CheckRemovalTree(root); err != nil {
		return nil, err
	}
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
		if _, err := installArtifactInfo(root, name); err != nil {
			if errors.Is(err, errNotInstallArtifact) || errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		artifacts = append(artifacts, name)
	}
	sort.Strings(artifacts)
	return artifacts, nil
}

var artifactName = regexp.MustCompile(`^(\.(install|repair|current)-[0-9]+|\.download-[0-9]+\.tar\.gz|\.backup-(go[0-9]+\.[0-9]+(\.[0-9]+)?)-[0-9]+)$`)

func installArtifactInfo(root, name string) (os.FileInfo, error) {
	if filepath.Base(name) != name || (name != ".current.tmp" && !artifactName.MatchString(name)) {
		return nil, fmt.Errorf("%w: %s", errNotInstallArtifact, name)
	}
	if err := shared.CheckRemovalTree(root); err != nil {
		return nil, err
	}
	path := filepath.Join(root, name)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if name == ".current.tmp" || strings.HasPrefix(name, ".current-") {
		if info.Mode()&os.ModeSymlink == 0 || !managedLinkPath(root, path) {
			return nil, fmt.Errorf("临时链接不属于本工具，已保留: %s", path)
		}
	} else if strings.HasPrefix(name, ".download-") {
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("下载残留不是普通文件: %s", path)
		}
	} else if !info.IsDir() || (strings.HasPrefix(name, ".backup-") && !isManagedDirectory(path)) {
		return nil, fmt.Errorf("残留目录类型或 Go 备份归属不明: %s", path)
	}
	return info, nil
}

func removeInstallArtifact(root, name string, expected os.FileInfo) error {
	info, err := installArtifactInfo(root, name)
	if err != nil {
		return err
	}
	if expected == nil || !os.SameFile(expected, info) || expected.ModTime() != info.ModTime() || expected.Size() != info.Size() {
		return fmt.Errorf("Go 安装残留在确认期间变化，未删除: %s", name)
	}
	path := filepath.Join(root, name)
	if name == ".current.tmp" || strings.HasPrefix(name, ".current-") {
		return os.Remove(path) // Remove the validated link itself, never its target.
	}
	return shared.RemovePathTree(path)
}

func removeManagedVersion(root, version string, expected os.FileInfo) error {
	if !validVersion(version) || filepath.Base(version) != version {
		return fmt.Errorf("Go 版本名无效: %s", version)
	}
	path := filepath.Join(root, version)
	if err := shared.CheckRemovalTree(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if expected == nil || !os.SameFile(expected, info) || !isManagedDirectory(path) {
		return fmt.Errorf("Go 版本目录已变化或缺少托管标记，未删除: %s", path)
	}
	return shared.RemovePathTree(path)
}
