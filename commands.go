package main

import (
	"fmt"
	"strings"

	cleanupbackups "snail_tool/internal/cleanup/backups"
	"snail_tool/internal/cleanup/softwaredata"
	"snail_tool/internal/firewall"
	"snail_tool/internal/input"
	"snail_tool/internal/log"
	"snail_tool/internal/selfupdate"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
	"snail_tool/internal/version"
)

func runCommand(args []string) int {
	for _, arg := range args {
		if err := input.Text(arg); err != nil {
			log.Error("命令行参数无效：", err)
			return exitUsage
		}
	}

	var operation func() error
	var permissionError string
	switch strings.ToLower(args[0]) {
	case "--version", "-v", "version":
		fmt.Println(version.Info())
		return 0
	case "--help", "-h", "help":
		printUsage()
		return 0
	case "update":
		permissionError = "更新需要 root 权限，请使用 sudo snail update"
		operation = func() error { return selfupdate.Run() }
	case "uninstall", "--uninstall":
		permissionError = "卸载需要 root 权限，请使用 sudo snail uninstall"
		operation = uninstall
	case "clean-data":
		if len(args) != 2 || args[1] != "--yes" {
			log.Error("用法：snail clean-data --yes（永久删除整个 /var/lib/snailtool）")
			return exitUsage
		}
		permissionError = "删除软件数据目录需要 root 权限，请使用 sudo snail clean-data --yes"
		operation = softwaredata.DeleteDefault
	case "sync-docker-forward":
		permissionError = "补上 Docker 转发兼容规则需要 root 权限"
		operation = firewall.SyncDockerForward
	case "clean-backups":
		if len(args) > 2 || (len(args) == 2 && args[1] != "--yes") {
			log.Error("用法：snail clean-backups [--yes]")
			return exitUsage
		}
		permissionError = "清理备份需要 root 权限，请使用 sudo snail clean-backups"
		if len(args) == 2 {
			operation = cleanupbackups.DeleteAll
		} else {
			operation = func() error { return cleanupbackups.ConfirmAll(ui.New()) }
		}
	default:
		log.Error("未知参数：", args[0])
		printUsage()
		return exitUsage
	}

	if !system.IsRoot() {
		log.Error(permissionError)
		return exitFailure
	}
	return exitStatus(operation())
}

func uninstall() error {
	log.Info("正在从仓库获取程序管理脚本...")
	log.Info("管理脚本地址：", selfupdate.InstallScriptURL)
	return selfupdate.Run("--uninstall")
}

func printUsage() {
	fmt.Println("用法：snail [update|uninstall|clean-backups [--yes]|clean-data --yes|sync-docker-forward|--version|-v|version]")
}
