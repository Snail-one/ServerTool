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

func TestPromptHistorySkipsPagesThatWereNotShown(t *testing.T) {
	view := &UI{reader: bufio.NewReader(strings.NewReader("next\nq\nq\nparent-next\n"))}
	var prompts []string
	page := func(name string) func() error {
		return func() error {
			prompts = append(prompts, name)
			answer, err := view.Ask(name)
			if err == nil && IsReturnChoice(answer) {
				return ErrReturnToMenu
			}
			return err
		}
	}
	err := RunPromptSteps(
		PromptStep{Run: page("first")},
		PromptStep{Enabled: func() bool { return false }, Run: page("hidden")},
		PromptStep{Run: page("last")},
	)
	if !errors.Is(err, ErrReturnToMenu) || strings.Join(prompts, ",") != "first,last,first" {
		t.Fatalf("incorrect history: prompts=%v err=%v", prompts, err)
	}
	if next, err := view.Ask("parent"); err != nil || next != "parent-next" {
		t.Fatalf("next=%q err=%v", next, err)
	}
}

func TestPromptFailureAndExplicitCancellationStopTheForm(t *testing.T) {
	failure := errors.New("input unavailable")
	for _, cause := range []error{failure, ErrCancelAction} {
		err := RunPromptSteps(
			PromptStep{Run: func() error { return nil }},
			PromptStep{Run: func() error { return cause }},
			PromptStep{Run: func() error { t.Fatal("form continued after failure or cancellation"); return nil }},
		)
		want := cause
		if cause == ErrCancelAction {
			want = ErrReturnToMenu
		}
		if !errors.Is(err, want) {
			t.Fatalf("got %v, want %v", err, want)
		}
	}
}
