package system

import (
	"os"
	"path/filepath"
	"testing"
)

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
