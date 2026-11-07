package keys

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"snail_tool/internal/shared"
	"snail_tool/internal/system"
)

func TestKeyOperationCancellationStaysInKeyMenu(t *testing.T) {
	oldKey, newKey := generateTestPublicKey(t), generateTestPublicKey(t)
	for _, answer := range []string{"0", "q", "Q", "exit", "返回", ""} {
		for _, operation := range []struct {
			name, input string
		}{
			{"add", "1\n" + answer + "\n"},
			{"replace-selection", "2\n" + answer + "\n"},
			{"replace-key", "2\n1\n" + answer + "\n"},
			{"replace-confirmation", "2\n1\n" + newKey + "\n" + answer + "\n"},
			{"delete-selection", "4\n" + answer + "\n"},
			{"delete-confirmation", "4\n1\n" + answer + "\n"},
		} {
			t.Run(operation.name+"/answer="+answer, func(t *testing.T) {
				account := &system.Account{Home: t.TempDir()}
				path := filepath.Join(account.Home, ".ssh", "authorized_keys")
				if err := os.Mkdir(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				original := oldKey + "\n"
				if err := os.WriteFile(path, []byte(original), 0600); err != nil {
					t.Fatal(err)
				}
				// The first q after cancelling must leave the key menu. The
				// parent action must not pause or consume its next response.
				view := newUIWithInput(t, operation.input+"q\nparent-next\n")
				shared.RunAction(view, "SSH 公钥管理失败", func() error {
					return configureSSHAuthorizedKeys(view, account)
				})
				if next, err := view.Ask("父菜单："); err != nil || next != "parent-next" {
					t.Fatalf("子操作取消跨层返回或多余等待：next=%q err=%v", next, err)
				}
				if got := readTestFile(t, path); got != original {
					t.Fatalf("取消后公钥发生变化：%q", got)
				}
			})
		}
	}
}

func TestInterruptedEditorReturnsToKeyMenu(t *testing.T) {
	interrupted := exec.Command("sh", "-c", "exit 130").Run()
	if !system.IsInterrupted(interrupted) {
		t.Fatalf("expected interrupted editor exit: %v", interrupted)
	}
	restoreEditors(t, func(string) bool { return true }, func(string, ...string) error { return interrupted })
	for _, setup := range []bool{false, true} {
		t.Run(map[bool]string{false: "keys", true: "setup"}[setup], func(t *testing.T) {
			account := &system.Account{Home: t.TempDir(), UID: os.Getuid(), GID: os.Getgid()}
			choice := "3"
			if setup {
				choice = "2"
			}
			view := newUIWithInput(t, choice+"\nq\nparent-next\n")
			var err error
			if setup {
				err = ensureSSHAuthorizedKeys(view, account)
			} else {
				err = configureSSHAuthorizedKeys(view, account)
			}
			if !errors.Is(err, shared.ErrReturnToMenu) {
				t.Fatalf("expected menu return: %v", err)
			}
			if next, err := view.Ask("父菜单："); err != nil || next != "parent-next" {
				t.Fatalf("编辑器取消跨层返回：next=%q err=%v", next, err)
			}
		})
	}
}
