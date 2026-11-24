package ui

import (
	"bytes"
	"errors"
	"strings"

	"snail_tool/internal/input"
)

func AskChoice(view *UI, prompt, fallback string, choices map[string]string) (string, error) {
	return askChoice(view, prompt, fallback, choices, nil)
}

// AskMenuChoice redraws the menu when an interactive retry needs fresh input.
func AskMenuChoice(view *UI, prompt, fallback string, choices map[string]string, draw func()) (string, error) {
	draw()
	return askChoice(view, prompt, fallback, choices, draw)
}

func askChoice(view *UI, prompt, fallback string, choices map[string]string, draw func()) (string, error) {
	if !strings.HasSuffix(prompt, "：") && !strings.HasSuffix(prompt, ":") {
		prompt += "："
	}
	redraw := draw != nil && canClearScreen()
	retrying := false
	for {
		var value string
		var err error
		buffered, _ := view.reader.Peek(view.reader.Buffered())
		if retrying && bytes.Contains(buffered, []byte{'\n'}) {
			value, err = view.ReadLine()
			if errors.Is(err, input.ErrInvalid) {
				continue
			}
		} else {
			if retrying && redraw {
				ClearScreen()
				draw()
				InvalidChoice()
			}
			value, err = view.Ask(prompt)
		}
		if err != nil {
			return "", err
		}
		if IsReturnChoice(value) {
			return "", ErrReturnToMenu
		}
		if value == "" {
			value = fallback
		}
		if choice, ok := choices[strings.ToLower(value)]; ok {
			return choice, nil
		}
		if !retrying && !redraw {
			InvalidChoice()
		}
		retrying = true
	}
}

func ConfirmMenuChange(view *UI, question, parent string) (bool, error) {
	PrintField("取消", "回车或 n 返回"+parent+"，0/q 返回上一步")
	return view.Confirm(question + "(y/N)：")
}
