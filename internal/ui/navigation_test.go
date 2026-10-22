package ui

import (
	"bufio"
	"errors"
	"strings"
	"testing"
)

func TestConfirmationReturnPreservesFollowingMenuInput(t *testing.T) {
	for _, answer := range []string{"0", "q", "Q", "exit", " EXIT ", "返回"} {
		t.Run(answer, func(t *testing.T) {
			view := &UI{reader: bufio.NewReader(strings.NewReader(answer + "\nnext\n"))}
			confirmed, err := view.Confirm("确认执行？(y/N)：")
			if confirmed || !errors.Is(err, ErrReturnToMenu) {
				t.Fatalf("return must cancel the pending action: confirmed=%v err=%v", confirmed, err)
			}
			if next, err := view.Ask("下一项："); err != nil || next != "next" {
				t.Fatalf("return consumed following menu input: next=%q err=%v", next, err)
			}
		})
	}
}

func TestDecliningOptionalConfirmationRemainsAChoice(t *testing.T) {
	for _, answer := range []string{"", "n", "no"} {
		view := &UI{reader: bufio.NewReader(strings.NewReader(answer + "\n"))}
		if confirmed, err := view.Confirm("是否启用可选设置？(y/N)："); confirmed || err != nil {
			t.Fatalf("optional decline must remain false without navigation: %q confirmed=%v err=%v", answer, confirmed, err)
		}
	}
}
