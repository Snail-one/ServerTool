package shared

import (
	"errors"

	"snail_tool/internal/log"
	"snail_tool/internal/ui"
)

var ErrReturnToMenu = ui.ErrReturnToMenu

func IsReturnChoice(choice string) bool {
	return ui.IsReturnChoice(choice)
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
