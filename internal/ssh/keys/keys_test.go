package keys

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/shared"
	"snail_tool/internal/system"
)

func TestEnsureSSHAuthorizedKeysAllowsQToReturn(t *testing.T) {
	home := t.TempDir()
	account := &system.Account{Name: "test", Home: home}
	view := newUIWithInput(t, "q\n")

	err := ensureSSHAuthorizedKeys(view, account)
	if !errors.Is(err, shared.ErrReturnToMenu) {
		t.Fatalf("输入 q 应返回菜单，得到：%v", err)
	}
	if system.DirExists(filepath.Join(home, ".ssh")) {
		t.Fatal("取消一键配置时不应创建 .ssh 目录")
	}
}

func TestEnsureSSHAuthorizedKeysOpensEditor(t *testing.T) {
	home := t.TempDir()
	account := &system.Account{Name: "test", Home: home, UID: os.Getuid(), GID: os.Getgid()}
	validKey := generateTestPublicKey(t)
	restoreEditors(t, func(name string) bool { return name == "vim" }, func(name string, args ...string) error {
		if name != "vim" || len(args) != 1 {
			t.Fatalf("unexpected editor launch: %s %v", name, args)
		}
		return os.WriteFile(args[0], []byte(validKey+"\n"), 0600)
	})
	view := newUIWithInput(t, "2\n\n")

	if err := ensureSSHAuthorizedKeys(view, account); err != nil {
		t.Fatalf("使用 vim 添加公钥失败：%v", err)
	}
	if !IsConfigured(account) {
		t.Fatal("编辑器保存的公钥未被识别")
	}
}

func TestOpenAuthorizedKeysWithEditorWritesFile(t *testing.T) {
	home := t.TempDir()
	account := &system.Account{Name: "test", Home: home, UID: os.Getuid(), GID: os.Getgid()}
	validKey := generateTestPublicKey(t)
	restoreEditors(t, func(name string) bool { return name == "nano" }, func(name string, args ...string) error {
		if name != "nano" || len(args) != 1 {
			t.Fatalf("unexpected editor launch: %s %v", name, args)
		}
		return os.WriteFile(args[0], []byte(validKey+"\n"), 0600)
	})

	if err := openAuthorizedKeysWithEditor(newUIWithInput(t, ""), account, "nano"); err != nil {
		t.Fatal(err)
	}
	content := readTestFile(t, filepath.Join(home, ".ssh", "authorized_keys"))
	if !strings.Contains(content, validKey) {
		t.Fatalf("编辑器内容未写入 authorized_keys：\n%s", content)
	}
}

func TestOpenAuthorizedKeysWithEditorMissingEditor(t *testing.T) {
	home := t.TempDir()
	account := &system.Account{Name: "test", Home: home}
	restoreEditors(t, func(string) bool { return false }, func(string, ...string) error {
		t.Fatal("未安装的编辑器不应被启动")
		return nil
	})

	if err := openAuthorizedKeysWithEditor(newUIWithInput(t, ""), account, "vim"); err != nil {
		t.Fatal(err)
	}
	if system.DirExists(filepath.Join(home, ".ssh")) {
		t.Fatal("未安装编辑器时不应创建 .ssh 目录")
	}
}

func TestOpenAuthorizedKeysWithEditorWarnsInvalidLines(t *testing.T) {
	home := t.TempDir()
	account := &system.Account{Name: "test", Home: home, UID: os.Getuid(), GID: os.Getgid()}
	restoreEditors(t, func(string) bool { return true }, func(_ string, args ...string) error {
		return os.WriteFile(args[0], []byte("123123\n"), 0600)
	})

	if err := openAuthorizedKeysWithEditor(newUIWithInput(t, "\n"), account, "vi"); err != nil {
		t.Fatal(err)
	}
	if got := readTestFile(t, filepath.Join(home, ".ssh", "authorized_keys")); !strings.Contains(got, "123123") {
		t.Fatalf("编辑器保存的内容应保留：\n%s", got)
	}
}

func TestEditorFromChoice(t *testing.T) {
	for _, item := range []struct {
		raw    string
		editor string
		ok     bool
	}{
		{raw: "", editor: "vim", ok: true},
		{raw: "1", editor: "vim", ok: true},
		{raw: "2", editor: "nano", ok: true},
		{raw: "3", editor: "vi", ok: true},
		{raw: " 2 ", editor: "nano", ok: true},
		{raw: "9", ok: false},
		{raw: "vim", ok: false},
	} {
		editor, ok := editorFromChoice(item.raw)
		if ok != item.ok || editor != item.editor {
			t.Fatalf("editorFromChoice(%q) = %q %v, want %q %v", item.raw, editor, ok, item.editor, item.ok)
		}
	}
}

func TestChooseAuthorizedKeyEditorDefaultsToVim(t *testing.T) {
	restoreEditors(t, func(string) bool { return true }, nil)
	editor, err := chooseAuthorizedKeyEditor(newUIWithInput(t, "\n"))
	if err != nil || editor != "vim" {
		t.Fatalf("直接回车应默认 vim，得到：%q %v", editor, err)
	}
}

func TestChooseAuthorizedKeyEditorSelectsNano(t *testing.T) {
	restoreEditors(t, func(string) bool { return true }, nil)
	editor, err := chooseAuthorizedKeyEditor(newUIWithInput(t, "2\n"))
	if err != nil || editor != "nano" {
		t.Fatalf("选择 2 应为 nano，得到：%q %v", editor, err)
	}
}

func TestChooseAuthorizedKeyEditorAllowsReturn(t *testing.T) {
	editor, err := chooseAuthorizedKeyEditor(newUIWithInput(t, "q\n"))
	if err != nil || editor != "" {
		t.Fatalf("输入 q 应返回上层，得到：%q %v", editor, err)
	}
}

func TestChooseAuthorizedKeyEditorRetriesMissingDefault(t *testing.T) {
	restoreEditors(t, func(name string) bool { return name == "nano" }, nil)
	editor, err := chooseAuthorizedKeyEditor(newUIWithInput(t, "\n2\n"))
	if err != nil || editor != "nano" {
		t.Fatalf("默认 vim 未安装时应能改选 nano，得到：%q %v", editor, err)
	}
}

func TestAddSSHAuthorizedKeysRetriesInvalidKey(t *testing.T) {
	home := t.TempDir()
	account := &system.Account{Name: "test", Home: home, UID: os.Getuid(), GID: os.Getgid()}
	validKey := generateTestPublicKey(t)
	view := newUIWithInput(t, "not-a-key\n"+validKey+"\n\n")

	if err := addSSHAuthorizedKeys(view, account); err != nil {
		t.Fatalf("无效公钥应提示后重试，不应返回错误：%v", err)
	}

	content := readTestFile(t, filepath.Join(home, ".ssh", "authorized_keys"))
	if !strings.Contains(content, validKey) {
		t.Fatalf("重试后未写入有效公钥：\n%s", content)
	}
	if strings.Contains(content, "not-a-key") {
		t.Fatalf("无效公钥不应写入 authorized_keys：\n%s", content)
	}
}

func TestAddSSHAuthorizedKeysEmptyInputReturnsWithoutError(t *testing.T) {
	home := t.TempDir()
	account := &system.Account{Name: "test", Home: home}
	view := newUIWithInput(t, "bad-key\n\n")

	if err := addSSHAuthorizedKeys(view, account); err != nil {
		t.Fatalf("无效公钥后回车结束应留在当前菜单，得到：%v", err)
	}
	if system.DirExists(filepath.Join(home, ".ssh")) {
		t.Fatal("未成功添加公钥时不应创建 .ssh 目录")
	}
}

func TestReplaceAuthorizedKeyIndexKeepsSurroundingKeys(t *testing.T) {
	content := "ssh-ed25519 AAAAmanual-one one@example\n" +
		sshAuthorizedKeysBegin + "\n" +
		"ssh-rsa AAAAmanaged managed@example\n" +
		sshAuthorizedKeysEnd + "\n" +
		"ssh-ed25519 AAAAmanual-two two@example\n"

	got := replaceAuthorizedKeyIndex(content, 2, "ssh-ed25519 AAAAreplaced replaced@example")
	if !strings.Contains(got, "AAAAmanual-one") || !strings.Contains(got, "AAAAmanual-two") {
		t.Fatalf("unselected keys were changed:\n%s", got)
	}
	if strings.Contains(got, "AAAAmanaged") {
		t.Fatalf("selected key remained:\n%s", got)
	}
	if !strings.Contains(got, sshAuthorizedKeysBegin) || !strings.Contains(got, "AAAAreplaced") {
		t.Fatalf("replacement was not written into the managed block:\n%s", got)
	}
}

func TestParseSingleAuthorizedKeyIndex(t *testing.T) {
	got, err := parseSingleAuthorizedKeyIndex("2", 3)
	if err != nil || got != 2 {
		t.Fatalf("got %d %v, want 2", got, err)
	}
	if _, err := parseSingleAuthorizedKeyIndex("1,2", 3); err == nil {
		t.Fatal("multiple indexes should be rejected when replacing")
	}
}

func TestReplaceSSHAuthorizedKeyRetriesThenReplaces(t *testing.T) {
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	if err := os.Mkdir(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	oldKey := generateTestPublicKey(t)
	newKey := generateTestPublicKey(t)
	authKeys := filepath.Join(sshDir, "authorized_keys")
	content := shared.FormatManagedBlock(sshAuthorizedKeysBegin, oldKey, sshAuthorizedKeysEnd)
	if err := os.WriteFile(authKeys, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	account := &system.Account{Name: "test", Home: home, UID: os.Getuid(), GID: os.Getgid()}
	view := newUIWithInput(t, "9\n1\nbad-key\n"+newKey+"\ny\n")

	if err := replaceSSHAuthorizedKey(view, account); err != nil {
		t.Fatalf("修改公钥失败：%v", err)
	}

	got := readTestFile(t, authKeys)
	if strings.Contains(got, oldKey) {
		t.Fatalf("原公钥仍在文件中：\n%s", got)
	}
	if !strings.Contains(got, newKey) {
		t.Fatalf("新公钥未写入：\n%s", got)
	}
	if !strings.Contains(got, sshAuthorizedKeysBegin) {
		t.Fatalf("替换后丢失本工具管理标记：\n%s", got)
	}
}

func TestDeleteSSHAuthorizedKeysRetriesInvalidSelection(t *testing.T) {
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	if err := os.Mkdir(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	authKeys := filepath.Join(sshDir, "authorized_keys")
	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIkeep keep@example"
	if err := os.WriteFile(authKeys, []byte(key+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	account := &system.Account{Name: "test", Home: home}
	view := newUIWithInput(t, "99\nabc\n1\ny\n")

	if err := deleteSSHAuthorizedKeys(view, account); err != nil {
		t.Fatalf("无效编号应提示后重试，不应返回错误：%v", err)
	}
	if strings.Contains(readTestFile(t, authKeys), "AAAAC3NzaC1lZDI1NTE5AAAAIkeep") {
		t.Fatal("重试选择后公钥应被删除")
	}
}

func TestAuthorizedKeyEntriesMarksManagedKeys(t *testing.T) {
	content := "ssh-ed25519 AAAAmanual user@example\n\n" +
		sshAuthorizedKeysBegin + "\n" +
		"ssh-rsa AAAAmanaged managed@example\n" +
		sshAuthorizedKeysEnd + "\n"

	entries := authorizedKeyEntries(content)
	if len(entries) != 2 {
		t.Fatalf("expected 2 authorized key entries, got %#v", entries)
	}
	if entries[0].index != 1 || entries[0].managed || entries[0].line != "ssh-ed25519 AAAAmanual user@example" {
		t.Fatalf("unexpected manual entry: %#v", entries[0])
	}
	if entries[1].index != 2 || !entries[1].managed || entries[1].line != "ssh-rsa AAAAmanaged managed@example" {
		t.Fatalf("unexpected managed entry: %#v", entries[1])
	}
}

func TestIsConfiguredRequiresAuthorizedKeyEntry(t *testing.T) {
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	if err := os.Mkdir(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sshDir, "authorized_keys")
	account := &system.Account{Name: "test", Home: home}

	if err := os.WriteFile(path, []byte("# comments do not provide SSH access\n\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if IsConfigured(account) {
		t.Fatal("comment-only authorized_keys was treated as configured")
	}
	if err := os.WriteFile(path, []byte("this is not an SSH public key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if IsConfigured(account) {
		t.Fatal("unrecognized authorized_keys content was treated as configured")
	}

	if err := os.WriteFile(path, []byte("ssh-ed25519 AAAAtest user@example\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if !IsConfigured(account) {
		t.Fatal("authorized key entry was not detected")
	}
}

func TestParseAuthorizedKeySelection(t *testing.T) {
	got, err := parseAuthorizedKeySelection("2, 1 2", 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{2, 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selection mismatch: got %#v, want %#v", got, want)
	}

	if _, err := parseAuthorizedKeySelection("4", 3); err == nil {
		t.Fatal("expected out-of-range selection to fail")
	}
}

func TestRemoveAuthorizedKeyIndexesRemovesSelectedKeysOnly(t *testing.T) {
	content := "ssh-ed25519 AAAAmanual-one one@example\n" +
		sshAuthorizedKeysBegin + "\n" +
		"ssh-rsa AAAAmanaged managed@example\n" +
		sshAuthorizedKeysEnd + "\n" +
		"ssh-ed25519 AAAAmanual-two two@example\n"

	cleaned := removeAuthorizedKeyIndexes(content, map[int]struct{}{
		2: {},
		3: {},
	})

	if strings.Contains(cleaned, "AAAAmanaged") || strings.Contains(cleaned, "AAAAmanual-two") {
		t.Fatalf("selected keys remained:\n%s", cleaned)
	}
	if strings.Contains(cleaned, sshAuthorizedKeysBegin) || strings.Contains(cleaned, sshAuthorizedKeysEnd) {
		t.Fatalf("empty managed key block remained:\n%s", cleaned)
	}
	if !strings.Contains(cleaned, "AAAAmanual-one") {
		t.Fatalf("unselected key was removed:\n%s", cleaned)
	}
}

func TestWriteManagedAuthorizedKeyIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "authorized_keys")
	existingKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIexisting user@example"
	firstKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIfirst snail@example"
	secondKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIsecond snail@example"

	if err := os.WriteFile(path, []byte(existingKey+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeManagedAuthorizedKey(path, readTestFile(t, path), firstKey); err != nil {
		t.Fatal(err)
	}
	if err := writeManagedAuthorizedKey(path, readTestFile(t, path), secondKey); err != nil {
		t.Fatal(err)
	}

	content := readTestFile(t, path)
	if strings.Count(content, sshAuthorizedKeysBegin) != 1 || strings.Count(content, sshAuthorizedKeysEnd) != 1 {
		t.Fatalf("managed SSH key block is not idempotent:\n%s", content)
	}
	wantBlock := shared.FormatManagedBlock(sshAuthorizedKeysBegin, firstKey+"\n"+secondKey, sshAuthorizedKeysEnd)
	if !strings.Contains(content, wantBlock) {
		t.Fatalf("managed SSH key block spacing mismatch:\n%s", content)
	}
	for _, key := range []string{existingKey, firstKey, secondKey} {
		if !strings.Contains(content, key) {
			t.Fatalf("missing key %q in:\n%s", key, content)
		}
	}
}
