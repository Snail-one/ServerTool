package golang

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"snail_tool/internal/shared"
	"snail_tool/internal/system"
)

func TestCleanupEnvironmentPreservesVersionsAndUnrelatedConfiguration(t *testing.T) {
	root, current, binDir := commandLinkTestInstall(t)
	if err := ensureGoCommandLinks(binDir, current); err != nil {
		t.Fatal(err)
	}
	account := &system.Account{Home: t.TempDir()}
	bashrc := filepath.Join(account.Home, ".bashrc")
	const manual = "export CUSTOM=keep\n"
	if err := os.WriteFile(bashrc, []byte(manual), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeManagedPath(bashrc); err != nil {
		t.Fatal(err)
	}
	if err := cleanupEnvironment(account, binDir, current); err != nil {
		t.Fatal(err)
	}
	if content := shared.ReadFileString(bashrc); strings.Contains(content, pathBegin) || !strings.Contains(content, manual) {
		t.Fatalf("unrelated shell configuration changed: %s", content)
	}
	for _, name := range []string{"go", "gofmt"} {
		if _, err := os.Lstat(filepath.Join(binDir, name)); !os.IsNotExist(err) {
			t.Fatalf("managed command link remains: %s %v", name, err)
		}
	}
	if !isManagedVersion(filepath.Join(root, "go1.24.1")) {
		t.Fatal("configuration cleanup must preserve installed Go versions")
	}
	if err := cleanupEnvironment(account, binDir, current); err != nil {
		t.Fatal(err)
	}
}
