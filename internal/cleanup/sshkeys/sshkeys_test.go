package sshkeys

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"snail_tool/internal/system"
)

func TestRunRemovesManagedBlockOnly(t *testing.T) {
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
	if err := Run(account); err != nil {
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

func TestRunKeepsEmptyFileAndDir(t *testing.T) {
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}

	authKeys := filepath.Join(sshDir, "authorized_keys")
	managedKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAImanaged snail@example"
	content := sshAuthorizedKeysBegin + "\n" + managedKey + "\n" + sshAuthorizedKeysEnd + "\n"
	if err := os.WriteFile(authKeys, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	account := &system.Account{Name: "test", Home: home}
	if err := Run(account); err != nil {
		t.Fatal(err)
	}

	if got := readTestFile(t, authKeys); got != "" {
		t.Fatalf("expected authorized_keys to be kept empty, got:\n%s", got)
	}
	if _, err := os.Stat(sshDir); err != nil {
		t.Fatalf("expected .ssh dir to be kept, stat err=%v", err)
	}
}

func TestRunKeepsNonEmptySSHDir(t *testing.T) {
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}

	authKeys := filepath.Join(sshDir, "authorized_keys")
	if err := os.WriteFile(authKeys, []byte(sshAuthorizedKeysBegin+"\nkey\n"+sshAuthorizedKeysEnd+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	knownHosts := filepath.Join(sshDir, "known_hosts")
	if err := os.WriteFile(knownHosts, []byte("example ssh-ed25519 AAAA\n"), 0600); err != nil {
		t.Fatal(err)
	}

	account := &system.Account{Name: "test", Home: home}
	if err := Run(account); err != nil {
		t.Fatal(err)
	}

	if got := readTestFile(t, authKeys); got != "" {
		t.Fatalf("expected authorized_keys to be kept empty, got:\n%s", got)
	}
	if _, err := os.Stat(knownHosts); err != nil {
		t.Fatalf("expected non-empty .ssh dir content to remain, stat err=%v", err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
