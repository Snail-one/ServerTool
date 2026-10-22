package cleanup

import (
	"errors"
	"os"
	"reflect"
	"testing"

	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

func TestCancelledCleanupDoesNotConsumeAnotherInput(t *testing.T) {
	for _, answer := range []string{"0", "q", "返回", "n", ""} {
		t.Run("answer="+answer, func(t *testing.T) {
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = reader.Close() })
			if _, err := writer.WriteString(answer + "\nnext\n"); err != nil {
				_ = writer.Close()
				t.Fatal(err)
			}
			_ = writer.Close()
			previous := os.Stdin
			os.Stdin = reader
			view := ui.New()
			os.Stdin = previous
			ran := false
			step := cleanupStep{label: "测试配置", run: func(*system.Account) error {
				ran = true
				return nil
			}}
			shared.RunAction(view, "清理失败", func() error {
				return runCleanupStepWithConfirm(view, &system.Account{}, step)
			})
			if ran {
				t.Fatal("cancelled cleanup executed an action")
			}
			if next, err := view.Ask("下一项："); err != nil || next != "next" {
				t.Fatalf("cancel/return consumed an extra input: next=%q err=%v", next, err)
			}
		})
	}
}

func TestAllConfigurationCleanupDeletesBackupsAfterSuccess(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failed configuration cleanup"}[fail], func(t *testing.T) {
			var calls []string
			steps := []cleanupStep{{name: "test", run: func(*system.Account) error {
				calls = append(calls, "configuration")
				if fail {
					return errors.New("test failure")
				}
				return nil
			}}}
			err := runAllCleanupSteps(&system.Account{}, steps, func() error {
				calls = append(calls, "backups")
				return nil
			})
			want := []string{"configuration", "backups"}
			if fail {
				want = []string{"configuration"}
			}
			if !reflect.DeepEqual(calls, want) || (err != nil) != fail {
				t.Fatalf("cleanup order=%v err=%v", calls, err)
			}
		})
	}
}
