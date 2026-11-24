package ui

import (
	"errors"

	"snail_tool/internal/log"
)

func RunAction(view *UI, failureMessage string, action func() error) {
	if err := action(); err != nil {
		if view.InputEnded() || errors.Is(err, ErrReturnToMenu) {
			return
		}
		log.Error(err)
		log.Error(failureMessage)
	}
	view.Pause()
}
