package cleanup

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
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

func TestAllConfigurationCleanupDeletesEntireDataDirectoryOnlyAfterSuccess(t *testing.T) {
	for _, scenario := range []string{"success", "failed configuration cleanup", "locked data directory"} {
		t.Run(scenario, func(t *testing.T) {
			root := seedSoftwareData(t)
			info, err := inspectSoftwareData(root)
			if err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(filepath.Dir(root), "preserve")
			if err := os.WriteFile(outside, []byte("unrelated data"), 0600); err != nil {
				t.Fatal(err)
			}
			if scenario == "locked data directory" {
				lock, err := os.OpenFile(filepath.Join(root, "backups/firewall/etc/.snailtool-backup.lock"), os.O_RDWR, 0)
				if err != nil {
					t.Fatal(err)
				}
				defer lock.Close()
				if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
					t.Fatal(err)
				}
				defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
			}
			var calls []string
			steps := []cleanupStep{{name: "test", run: func(*system.Account) error {
				calls = append(calls, "configuration")
				if got := shared.ReadFileString(filepath.Join(root, "backups/ups/config.original.json")); got != "keep until confirmed" {
					t.Fatal("restoration data deleted before configuration cleanup finished")
				}
				if scenario == "failed configuration cleanup" {
					return errors.New("test failure")
				}
				return nil
			}}}
			err = runAllCleanupSteps(&system.Account{}, steps, func() error {
				calls = append(calls, "data")
				return deleteSoftwareData(root, info)
			})
			want := []string{"configuration", "data"}
			if scenario == "failed configuration cleanup" {
				want = []string{"configuration"}
			}
			if !reflect.DeepEqual(calls, want) || (err != nil) != (scenario != "success") {
				t.Fatalf("cleanup order=%v err=%v", calls, err)
			}
			if scenario == "success" {
				if _, err := os.Lstat(root); !os.IsNotExist(err) {
					t.Fatalf("entire data directory remains: %v", err)
				}
			} else if got := shared.ReadFileString(filepath.Join(root, ".hidden")); got != "keep until confirmed" {
				t.Fatal("failed cleanup partially removed software data")
			}
			if scenario == "locked data directory" && !strings.Contains(err.Error(), "配置已清理，但软件数据目录删除未完成") {
				t.Fatal("directory deletion failure must report completed configuration cleanup")
			}
			if shared.ReadFileString(outside) != "unrelated data" {
				t.Fatal("cleanup affected files outside the software data directory")
			}
		})
	}
}
