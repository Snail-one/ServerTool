package ui

import (
	"errors"
	"strings"
)

// ErrReturnToMenu distinguishes navigation from completed or failed actions.
var ErrReturnToMenu = errors.New("return to menu")

// IsReturnChoice is shared by menus, value prompts, and confirmation prompts.
func IsReturnChoice(choice string) bool {
	switch strings.ToLower(strings.TrimSpace(choice)) {
	case "0", "q", "exit", "返回":
		return true
	default:
		return false
	}
}
