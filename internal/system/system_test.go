package system

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"snail_tool/internal/input"
)

func TestPortValidationUsesStrictDecimalInput(t *testing.T) {
	for _, item := range []struct {
		raw  string
		want int
	}{{"22", 22}, {" 00022 ", 22}, {"65535", 65535}} {
		if got, err := ValidatePort(item.raw); err != nil || got != item.want {
			t.Fatalf("port %q = %d, %v", item.raw, got, err)
		}
	}
	for _, raw := range []string{"", "+22", "0", "65536", "２２", "22\xff", "22\u200b", "22\r"} {
		if _, err := ValidatePort(raw); err == nil {
			t.Fatalf("invalid port accepted: %q", raw)
		}
	}
}

func TestPublicKeyRejectsCorruptionAndMultipleLinesBeforeExternalValidation(t *testing.T) {
	for _, raw := range []string{"ssh-ed25519 AAAA\xff", "ssh-ed25519 AAAA\u200b", "ssh-ed25519 AAAA\nssh-rsa BBBB", "ssh-ed25519 AAAA\x1b[31m"} {
		if err := ValidateSSHPublicKey(raw); !errors.Is(err, input.ErrInvalid) {
			t.Fatalf("invalid public key reached external validation: %q: %v", raw, err)
		}
	}
}

func TestRootCheckDoesNotTrustCommandsInPATH(t *testing.T) {
	dir := t.TempDir()
	fakeUID := "0"
	if os.Geteuid() == 0 {
		fakeUID = "12345"
	}
	if err := os.WriteFile(filepath.Join(dir, "id"), []byte("#!/bin/sh\nprintf '"+fakeUID+"\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if IsRoot() != (os.Geteuid() == 0) {
		t.Fatal("a command in PATH changed the permission check")
	}
}
