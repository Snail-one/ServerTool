package main

import (
	"os"

	"snail_tool/internal/app"
	"snail_tool/internal/log"
	"snail_tool/internal/system"
)

func main() {
	if !system.IsRoot() {
		log.Error("请使用 sudo 或 root 运行此工具")
		os.Exit(1)
	}

	if err := app.New().Run(); err != nil {
		log.Error(err)
		os.Exit(1)
	}
}
