package main

import (
	"os"

	"snail_tool/internal/app"
	"snail_tool/internal/app/startup"
	"snail_tool/internal/log"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
	"snail_tool/internal/version"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) != 0 {
		return runCommand(args)
	}
	return runInteractive()
}

func runInteractive() int {
	var isRoot bool
	report := startup.New(version.Version)
	report.Record("运行权限", func() {
		ui.NewDetectionProgress(os.Stdout).Step("运行权限", func() {
			isRoot = system.IsRoot()
		})
	})
	if !isRoot {
		log.Error("请使用 sudo 或 root 运行此工具")
		return exitFailure
	}

	return exitStatus(app.NewWithStartupReport(report).Run())
}

const (
	exitFailure = 1
	exitUsage   = 2
)

func exitStatus(err error) int {
	if err != nil {
		log.Error(err)
		return exitFailure
	}
	return 0
}
