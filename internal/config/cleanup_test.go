package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"snail_tool/internal/system"
)

func TestCleanupSSHAuthorizedKeysRemovesManagedBlockOnly(t *testing.T) {
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}

	authKeys := filepath.Join(sshDir, "authorized_keys")
	existingKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIexisting user@example"
	managedKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAImanaged snail@example"
	content := existingKey + "\n\n" +
		sshAuthorizedKeysBegin + "\n" +
		managedKey + "\n" +
		sshAuthorizedKeysEnd + "\n"
	if err := os.WriteFile(authKeys, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	account := &system.Account{Name: "test", Home: home}
	if err := cleanupSSHAuthorizedKeys(account); err != nil {
		t.Fatal(err)
	}

	got := readTestFile(t, authKeys)
	if strings.Contains(got, sshAuthorizedKeysBegin) || strings.Contains(got, managedKey) {
		t.Fatalf("managed SSH key block remained:\n%s", got)
	}
	if !strings.Contains(got, existingKey) {
		t.Fatalf("existing SSH key was removed:\n%s", got)
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
	for _, key := range []string{existingKey, firstKey, secondKey} {
		if !strings.Contains(content, key) {
			t.Fatalf("missing key %q in:\n%s", key, content)
		}
	}
}

func TestCleanupVimConfigRemovesOnlyManagedTemplate(t *testing.T) {
	home := t.TempDir()
	account := &system.Account{Name: "test", Home: home}
	vimrc := filepath.Join(home, ".vimrc")

	if err := os.WriteFile(vimrc, []byte(vimrcContent), 0644); err != nil {
		t.Fatal(err)
	}
	if err := cleanupVimConfig(account); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(vimrc); !os.IsNotExist(err) {
		t.Fatalf("expected managed vimrc to be removed, stat err=%v", err)
	}

	if err := os.WriteFile(vimrc, []byte("set number\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := cleanupVimConfig(account); err != nil {
		t.Fatal(err)
	}
	if got := readTestFile(t, vimrc); got != "set number\n" {
		t.Fatalf("modified vimrc should be preserved, got:\n%s", got)
	}
}

func TestCleanupBashConfigRemovesManagedBlocksOnly(t *testing.T) {
	home := t.TempDir()
	account := &system.Account{Name: "test", Home: home}
	bashrc := filepath.Join(home, ".bashrc")
	content := "export EDITOR=vim\n\n" +
		bashAliasBegin + "\n" + bashAliasBlock + "\n" + bashAliasEnd + "\n\n" +
		bashCommandBegin + "\nsnail() {\n  sudo '/usr/local/bin/snail_tool' \"$@\"\n}\n" + bashCommandEnd + "\n"
	if err := os.WriteFile(bashrc, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if err := cleanupBashConfig(account); err != nil {
		t.Fatal(err)
	}

	got := readTestFile(t, bashrc)
	for _, marker := range []string{bashAliasBegin, bashAliasEnd, bashCommandBegin, bashCommandEnd, "snail()"} {
		if strings.Contains(got, marker) {
			t.Fatalf("managed bash content remained:\n%s", got)
		}
	}
	if !strings.Contains(got, "export EDITOR=vim") {
		t.Fatalf("unrelated bash content was removed:\n%s", got)
	}
}

func TestCleanupProxyConfigRemovesManagedBlockAndCurrentEnv(t *testing.T) {
	clearProxyEnv(t)
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:8888")

	home := t.TempDir()
	account := &system.Account{Name: "test", Home: home}
	bashrc := filepath.Join(home, ".bashrc")
	content := "export EDITOR=vim\n\n" +
		proxyBegin + "\nexport http_proxy=\"http://127.0.0.1:8888\"\n" + proxyEnd + "\n"
	if err := os.WriteFile(bashrc, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if err := cleanupProxyConfig(account); err != nil {
		t.Fatal(err)
	}

	got := readTestFile(t, bashrc)
	if strings.Contains(got, proxyBegin) || strings.Contains(got, "http_proxy") {
		t.Fatalf("managed proxy content remained:\n%s", got)
	}
	if !strings.Contains(got, "export EDITOR=vim") {
		t.Fatalf("unrelated bash content was removed:\n%s", got)
	}
	if value := os.Getenv("HTTP_PROXY"); value != "" {
		t.Fatalf("HTTP_PROXY was not unset, got %q", value)
	}
}
