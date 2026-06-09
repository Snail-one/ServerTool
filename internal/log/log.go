package log

import (
	"fmt"
	"os"
)

const (
	green  = "\033[32m"
	yellow = "\033[33m"
	red    = "\033[31m"
	reset  = "\033[0m"
)

func Info(args ...any) {
	fmt.Println(green + "[INFO]" + reset + " " + fmt.Sprint(args...))
}

func Warn(args ...any) {
	fmt.Println(yellow + "[WARN]" + reset + " " + fmt.Sprint(args...))
}

func Error(args ...any) {
	fmt.Fprintln(os.Stderr, red+"[ERROR]"+reset+" "+fmt.Sprint(args...))
}
