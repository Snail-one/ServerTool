package main

import (
	"fmt"
	"os"
	"strings"

	"snail_tool/internal/app"
	"snail_tool/internal/log"
	"snail_tool/internal/selfupdate"
	"snail_tool/internal/system"
	"snail_tool/internal/version"
)

func main() {
	if handled := handleArgs(os.Args[1:]); handled {
		return
	}

	if !system.IsRoot() {
		log.Error("请使用 sudo 或 root 运行此工具")
		os.Exit(1)
	}

	if err := app.New().Run(); err != nil {
		log.Error(err)
		os.Exit(1)
	}
}

func handleArgs(args []string) bool {
	if len(args) == 0 {
		return false
	}

	switch strings.ToLower(args[0]) {
	case "--version", "-v", "version":
		fmt.Println(version.Info())
		return true
	case "--help", "-h", "help":
		printUsage()
		return true
	case "update":
		if !system.IsRoot() {
			log.Error("更新需要 root 权限，请使用 sudo snail update")
			os.Exit(1)
		}
		log.Info("正在从仓库获取安装更新脚本...")
		log.Info("更新脚本地址：", selfupdate.InstallScriptURL)
		if err := selfupdate.Run(); err != nil {
			log.Error(err)
			os.Exit(1)
		}
		return true
	default:
		log.Error("未知参数：", args[0])
		printUsage()
		os.Exit(2)
		return true
	}
}

func printUsage() {
	fmt.Println("用法：snail [update|--version|-v|version]")
}
