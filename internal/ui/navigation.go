package ui

import (
	"errors"
	"strings"
)

// ErrReturnToMenu asks the owning page to return to its previous page or menu.
var ErrReturnToMenu = errors.New("return to menu")

// ErrCancelAction declines an entire action rather than stepping back a page.
var ErrCancelAction = errors.New("cancel action")

// PromptStep describes one input page before an operation is committed.
// Enabled lets a choice omit pages (for example, IPv6 input for IPv4 rules).
type PromptStep struct {
	Enabled func() bool
	Run     func() error
}

// RunPromptSteps handles back at the page that owns the input. Only returning
// from the first visible page propagates to the calling menu. Steps must only
// collect input; perform writes and commands after this function succeeds.
func RunPromptSteps(steps ...PromptStep) error {
	history := []int{}
	for index := 0; index < len(steps); {
		step := steps[index]
		if step.Enabled != nil && !step.Enabled() {
			index++
			continue
		}
		err := step.Run()
		if errors.Is(err, ErrCancelAction) {
			return ErrReturnToMenu
		}
		if errors.Is(err, ErrReturnToMenu) {
			if len(history) == 0 {
				return err
			}
			index = history[len(history)-1]
			history = history[:len(history)-1]
			continue
		}
		if err != nil {
			return err
		}
		history = append(history, index)
		index++
	}
	return nil
}

// IsReturnChoice is shared by menus, value prompts, and confirmation prompts.
func IsReturnChoice(choice string) bool {
	switch strings.ToLower(strings.TrimSpace(choice)) {
	case "0", "q", "exit", "返回":
		return true
	default:
		return false
	}
}
