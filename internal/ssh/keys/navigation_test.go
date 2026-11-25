package keys

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"snail_tool/internal/storage"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

func TestDeletingLastKeyShowsImpactAndWaitsBeforeRefreshing(t *testing.T) {
	account := &system.Account{Name: "test-user", Home: t.TempDir()}
	path := authorizedKeysPath(account)
	if err := os.Mkdir(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(generateTestPublicKey(t)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	output, err := os.CreateTemp(t.TempDir(), "key-menu")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	view := newUIWithInput(t, "1\nd\ny\n\nq\nparent-next\n")
	previous := os.Stdout
	os.Stdout = output
	err = configureSSHAuthorizedKeys(view, account)
	os.Stdout = previous
	if !errors.Is(err, ui.ErrReturnToMenu) {
		t.Fatalf("expected menu return: %v", err)
	}
	if next, err := view.Ask("父菜单"); err != nil || next != "parent-next" {
		t.Fatalf("unexpected navigation: next=%q err=%v", next, err)
	}
	menu := readTestFile(t, output.Name())
	for _, text := range []string{"test-user", path, "即将删除列表中的全部公钥", "SSH 公钥删除完成", "按回车返回菜单", "0 把"} {
		if !strings.Contains(menu, text) {
			t.Errorf("key menu is missing %q", text)
		}
	}
	if IsConfigured(account) {
		t.Fatal("the selected key is still present")
	}
}

func TestClearKeyShortcutPreservesNonKeyLines(t *testing.T) {
	account := &system.Account{Home: t.TempDir()}
	path := authorizedKeysPath(account)
	if err := os.Mkdir(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	first, second := generateTestPublicKey(t), generateTestPublicKey(t)
	content := "# outside note\n" + first + "\n" +
		storage.FormatManagedBlock(sshAuthorizedKeysBegin, "# inside note\n"+second, sshAuthorizedKeysEnd) +
		"custom entry\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	view := newUIWithInput(t, "X\ny\n\nq\n")
	if err := configureSSHAuthorizedKeys(view, account); !errors.Is(err, ui.ErrReturnToMenu) {
		t.Fatalf("unexpected navigation error: %v", err)
	}
	if IsConfigured(account) {
		t.Fatal("clear shortcut left a listed public key")
	}
	remaining := readTestFile(t, path)
	for _, line := range []string{"# outside note", "# inside note", "custom entry"} {
		if !strings.Contains(remaining, line) {
			t.Errorf("clearing public keys removed %q", line)
		}
	}
}

func TestKeyOperationCancellationStaysInKeyMenu(t *testing.T) {
	oldKey, newKey := generateTestPublicKey(t), generateTestPublicKey(t)
	for _, answer := range []string{"0", "q", "Q", "exit", "返回", ""} {
		replaceBacks := "q\nq\n"
		if answer == "" {
			replaceBacks = "q\n"
		}
		for _, operation := range []struct {
			name, input string
		}{
			{"add", "A\n" + answer + "\n"},
			{"replace-key", "1\nr\n" + answer + "\nq\n"},
			{"replace-confirmation", "1\nr\n" + newKey + "\n" + answer + "\n" + replaceBacks},
			{"delete-confirmation", "1\nd\n" + answer + "\nq\n"},
			{"clear-confirmation", "x\n" + answer + "\n"},
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
				// Walk back through every selection page, then leave the key
				// menu without consuming the next parent response.
				view := newUIWithInput(t, operation.input+"q\nparent-next\n")
				ui.RunAction(view, "SSH 公钥管理失败", func() error {
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
			choice := "v"
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
			if !errors.Is(err, ui.ErrReturnToMenu) {
				t.Fatalf("expected menu return: %v", err)
			}
			if next, err := view.Ask("父菜单："); err != nil || next != "parent-next" {
				t.Fatalf("编辑器取消跨层返回：next=%q err=%v", next, err)
			}
		})
	}
}
