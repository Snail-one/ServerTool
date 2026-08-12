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
	"snail_tool/internal/ui"
)

func TestEnsureSSHAuthorizedKeysAllowsQToReturn(t *testing.T) {
	home := t.TempDir()
	account := &system.Account{Name: "test", Home: home}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.WriteString("q\n"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	previousStdin := os.Stdin
	os.Stdin = reader
	view := ui.New()
	os.Stdin = previousStdin
	t.Cleanup(func() { _ = reader.Close() })

	err = ensureSSHAuthorizedKeys(view, account)
	if !errors.Is(err, shared.ErrReturnToMenu) {
		t.Fatalf("输入 q 应返回菜单，得到：%v", err)
	}
	if system.DirExists(filepath.Join(home, ".ssh")) {
		t.Fatal("取消一键配置时不应创建 .ssh 目录")
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
