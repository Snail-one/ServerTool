package golang

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"snail_tool/internal/log"
	"snail_tool/internal/ui"
)

func installRelease(item release) error {
	log.Info("准备安装 Go 版本：", item.Version)
	fileArch, err := supportedArch(runtime.GOARCH)
	if err != nil {
		return err
	}
	archive, ok := archiveFor(item, fileArch)
	if !ok {
		return fmt.Errorf("%s 没有适用于 linux/%s 的官方归档", item.Version, runtime.GOARCH)
	}
	if err := os.MkdirAll(installRoot, 0755); err != nil {
		return fmt.Errorf("创建 Go 安装目录失败: %w", err)
	}
	destination := filepath.Join(installRoot, item.Version)
	if info, err := os.Stat(destination); err == nil && info.IsDir() {
		if !isManagedVersion(destination) {
			return fmt.Errorf("%s 已存在但不是本工具管理的有效 Go 版本目录，拒绝覆盖", destination)
		}
		log.Info(item.Version, " 已安装，直接切换")
	} else if err == nil {
		return fmt.Errorf("%s 已存在且不是目录，拒绝覆盖", destination)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	} else {
		if err := downloadAndInstall(archive, destination); err != nil {
			return err
		}
	}
	log.Info("切换当前版本软链接：", currentLink, " -> ", destination)
	if err := activateVersion(installRoot, currentLink, item.Version); err != nil {
		return err
	}
	log.Info("更新目标用户 ~/.bashrc 中的 Go PATH...")
	if err := configureTargetUserPath(); err != nil {
		return err
	}
	log.Info("Go PATH 配置完成")
	ui.PrintSuccessCard("Go 安装完成",
		ui.CardField{Label: "当前版本", Value: item.Version},
		ui.CardField{Label: "安装位置", Value: destination},
		ui.CardField{Label: "立即生效", Value: "source ~/.bashrc"},
	)
	return nil
}

func reinstallRelease(item release) error {
	fileArch, err := supportedArch(runtime.GOARCH)
	if err != nil {
		return err
	}
	archive, ok := archiveFor(item, fileArch)
	if !ok {
		return fmt.Errorf("%s 没有适用于 linux/%s 的官方归档", item.Version, runtime.GOARCH)
	}
	destination := filepath.Join(installRoot, item.Version)
	if !isManagedVersion(destination) {
		return fmt.Errorf("当前版本目录不是本工具管理的有效 Go 版本：%s", destination)
	}
	replacement, err := unusedTempPath(installRoot, ".repair-*")
	if err != nil {
		return fmt.Errorf("创建修复临时路径失败: %w", err)
	}
	defer os.RemoveAll(replacement)
	log.Info("下载并准备当前版本的全新副本...")
	if err := downloadAndInstall(archive, replacement); err != nil {
		return err
	}
	log.Info("下载、校验和解压完成，开始替换当前版本目录...")
	if err := replaceManagedVersion(destination, replacement); err != nil {
		return err
	}
	log.Info("重新建立当前版本软链接：", currentLink, " -> ", destination)
	if err := activateVersion(installRoot, currentLink, item.Version); err != nil {
		return err
	}
	log.Info("重新写入目标用户 ~/.bashrc 中的 Go PATH...")
	if err := configureTargetUserPath(); err != nil {
		return err
	}
	ui.PrintSuccessCard("Go 版本和 PATH 修复完成",
		ui.CardField{Label: "当前版本", Value: item.Version},
		ui.CardField{Label: "安装位置", Value: destination},
		ui.CardField{Label: "立即生效", Value: "source ~/.bashrc"},
	)
	return nil
}

func downloadAndInstall(file releaseFile, destination string) error {
	log.Info("选定官方归档：", file.Filename)
	archivePath, err := downloadArchive(downloadBase+file.Filename, file.SHA256)
	if err != nil {
		return err
	}
	defer os.Remove(archivePath)

	staging, err := os.MkdirTemp(installRoot, ".install-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	log.Info("解压已校验归档到临时目录...")
	if err := extractGoArchive(archivePath, staging); err != nil {
		return fmt.Errorf("解压 Go 归档失败: %w", err)
	}
	extracted := filepath.Join(staging, "go")
	if info, err := os.Stat(filepath.Join(extracted, "bin", "go")); err != nil || info.IsDir() {
		return errors.New("Go 归档缺少 go/bin/go，已取消安装")
	}
	if err := os.WriteFile(filepath.Join(extracted, managedFile), []byte("managed by ServerTool\n"), 0644); err != nil {
		return fmt.Errorf("写入 Go 管理标记失败: %w", err)
	}
	log.Info("归档结构检查完成，写入版本目录：", destination)
	if err := os.Rename(extracted, destination); err != nil {
		return fmt.Errorf("写入 Go 版本目录失败: %w", err)
	}
	log.Info("版本目录写入完成")
	return nil
}

func unusedTempPath(root, pattern string) (string, error) {
	path, err := os.MkdirTemp(root, pattern)
	if err != nil {
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return path, nil
}

func replaceManagedVersion(destination, replacement string) error {
	if !isManagedVersion(destination) || !isManagedVersion(replacement) {
		return errors.New("拒绝替换无效或非托管的 Go 版本目录")
	}
	backup, err := unusedTempPath(filepath.Dir(destination), ".backup-"+filepath.Base(destination)+"-*")
	if err != nil {
		return fmt.Errorf("创建版本备份路径失败: %w", err)
	}
	if err := os.Rename(destination, backup); err != nil {
		return fmt.Errorf("备份当前 Go 版本失败: %w", err)
	}
	if err := os.Rename(replacement, destination); err != nil {
		if rollbackErr := os.Rename(backup, destination); rollbackErr != nil {
			return fmt.Errorf("替换 Go 版本失败: %v；恢复原版本也失败: %w", err, rollbackErr)
		}
		return fmt.Errorf("替换 Go 版本失败，已恢复原版本: %w", err)
	}
	if err := os.RemoveAll(backup); err != nil {
		log.Warn("新版本已生效，但清理旧版本备份失败：", backup, "：", err)
	}
	return nil
}
