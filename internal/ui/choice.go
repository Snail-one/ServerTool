package ui

import "strings"

func AskChoice(view *UI, prompt, fallback string, choices map[string]string) (string, error) {
	if !strings.HasSuffix(prompt, "：") && !strings.HasSuffix(prompt, ":") {
		prompt += "："
	}
	for {
		value, err := view.Ask(prompt)
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
		InvalidChoice()
	}
}

func ConfirmMenuChange(view *UI, question, parent string) (bool, error) {
	PrintField("取消", "回车或 n 返回"+parent+"，0/q 返回上一步")
	return view.Confirm(question + "(y/N)：")
}
