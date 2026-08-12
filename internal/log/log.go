package log

import (
	"fmt"
	"os"
	"strings"
)

const (
	blue   = "\033[34m"
	yellow = "\033[33m"
	red    = "\033[31m"
	reset  = "\033[0m"
)

func Info(args ...any) {
	write(os.Stdout, blue, "信息", args...)
}

func Warn(args ...any) {
	write(os.Stdout, yellow, "警告", args...)
}

func Error(args ...any) {
	write(os.Stderr, red, "错误", args...)
}

func write(output *os.File, color, level string, args ...any) {
	prefix := "[" + level + "]"
	if colorEnabled(output) {
		prefix = color + prefix + reset
	}
	fmt.Fprintln(output, prefix+" "+fmt.Sprint(args...))
}

func colorEnabled(output *os.File) bool {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled || strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return false
	}
	if forced := os.Getenv("CLICOLOR_FORCE"); forced != "" && forced != "0" {
		return true
	}
	info, err := output.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
