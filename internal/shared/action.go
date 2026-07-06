package shared

import (
	"errors"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/ui"
)

var ErrReturnToMenu = errors.New("return to menu")

func IsReturnChoice(choice string) bool {
	switch strings.ToLower(strings.TrimSpace(choice)) {
	case "0", "q", "exit":
		return true
	default:
		return false
	}
}

func RunAction(view *ui.UI, failureMessage string, action func() error) {
	if err := action(); err != nil {
		if errors.Is(err, ErrReturnToMenu) {
			return
		}
		log.Error(err)
		log.Error(failureMessage)
	}
	view.Pause()
}
