//go:build !linux

package ui

import (
	"fmt"
	"os"
)

func hideInputEcho(input *os.File) (func(), error) {
	if input == nil {
		return nil, nil
	}
	info, err := input.Stat()
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeCharDevice != 0 {
		return nil, fmt.Errorf("当前平台不支持隐藏终端输入")
	}
	return nil, nil
}
