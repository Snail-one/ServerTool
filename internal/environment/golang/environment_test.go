package golang

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfficialInstallDetectionAndRemoval(t *testing.T) {
	root := filepath.Join(t.TempDir(), "go")
	if officialInstallDetected(root) {
		t.Fatal("empty path should not be detected as an official install")
	}
	goBinary := filepath.Join(root, "bin", "go")
	if err := os.MkdirAll(filepath.Dir(goBinary), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goBinary, []byte("binary"), 0755); err != nil {
		t.Fatal(err)
	}
	if !officialInstallDetected(root) {
		t.Fatal("expected Go binary to be detected")
	}
	if err := removeOfficialInstall(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("official install directory still exists: %v", err)
	}
}

func TestRemoveOfficialGoEnvFromBashrc(t *testing.T) {
	input := `export EDITOR=vim
export PATH="$PATH:/usr/local/go/bin"
export GOROOT=/usr/local/go
# export PATH="/usr/local/go/bin:$PATH"
alias goversion='/usr/local/go/bin/go version'
export GOPATH="$HOME/go"
export PATH="/opt/go/current/bin:$PATH"
`
	cleaned, changed := removeOfficialGoEnv(input)
	if !changed {
		t.Fatal("expected official Go environment lines to be detected")
	}
	if strings.Contains(cleaned, `PATH="$PATH:/usr/local/go/bin"`) || strings.Contains(cleaned, "GOROOT=/usr/local/go") {
		t.Fatalf("official Go environment remained:\n%s", cleaned)
	}
	for _, wanted := range []string{
		"export EDITOR=vim",
		`# export PATH="/usr/local/go/bin:$PATH"`,
		`alias goversion='/usr/local/go/bin/go version'`,
		`export GOPATH="$HOME/go"`,
		`export PATH="/opt/go/current/bin:$PATH"`,
	} {
		if !strings.Contains(cleaned, wanted) {
			t.Fatalf("unrelated line %q was removed:\n%s", wanted, cleaned)
		}
	}
}

func TestManagedPathIsIdempotentAndCleanable(t *testing.T) {
	bashrc := filepath.Join(t.TempDir(), ".bashrc")
	if err := os.WriteFile(bashrc, []byte("export EDITOR=vim\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeManagedPath(bashrc); err != nil {
		t.Fatal(err)
	}
	if err := writeManagedPath(bashrc); err != nil {
		t.Fatal(err)
	}
	content := readFile(t, bashrc)
	if strings.Count(content, pathBegin) != 1 || strings.Count(content, pathBody) != 1 {
		t.Fatalf("managed PATH block is not idempotent:\n%s", content)
	}
	if !strings.Contains(content, "export EDITOR=vim") {
		t.Fatalf("unrelated bashrc content was removed:\n%s", content)
	}
	changed, err := cleanupManagedPath(bashrc)
	if err != nil || !changed {
		t.Fatalf("cleanup = %v, %v", changed, err)
	}
	content = readFile(t, bashrc)
	if strings.Contains(content, pathBegin) || strings.Contains(content, pathBody) {
		t.Fatalf("managed PATH block remained:\n%s", content)
	}
	if !strings.Contains(content, "export EDITOR=vim") {
		t.Fatalf("unrelated bashrc content was removed:\n%s", content)
	}
}
