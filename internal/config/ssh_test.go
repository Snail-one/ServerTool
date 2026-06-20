package config

import (
	"reflect"
	"strings"
	"testing"
)

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
