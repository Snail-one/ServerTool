package config

import (
	"errors"
	"strings"
)

var ErrReturnToMenu = errors.New("return to menu")

func isReturnChoice(choice string) bool {
	switch strings.ToLower(strings.TrimSpace(choice)) {
	case "0", "q", "exit":
		return true
	default:
		return false
	}
}
