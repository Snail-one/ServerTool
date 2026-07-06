package ui

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

type UI struct {
	reader *bufio.Reader
}

func New() *UI {
	return &UI{reader: bufio.NewReader(os.Stdin)}
}

func (u *UI) Ask(prompt string) (string, error) {
	fmt.Print(prompt)
	value, err := u.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func (u *UI) Confirm(prompt string) (bool, error) {
	value, err := u.Ask(prompt)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(value, "y"), nil
}

func (u *UI) Pause() {
	u.PauseWithPrompt("按回车返回菜单...")
}

func (u *UI) PauseWithPrompt(prompt string) {
	fmt.Println()
	fmt.Print(prompt)
	_, _ = u.reader.ReadString('\n')
	fmt.Println()
}

func ClearScreen() {
	// \033[2J 清屏, \033[H 光标移到左上角, \033[3J 尝试清除回滚缓冲 (部分终端支持)
	fmt.Print("\033[2J\033[H\033[3J")
}
