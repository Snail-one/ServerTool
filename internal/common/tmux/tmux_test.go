package tmux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"snail_tool/internal/shared"
	"snail_tool/internal/system"
)

func TestCleanupRemovesManagedTmuxSettingsAndPreservesManualOnes(t *testing.T) {
	account := &system.Account{Home: t.TempDir(), UID: os.Getuid(), GID: os.Getgid()}
	path := filepath.Join(account.Home, ".tmux.conf")
	const manual = "set -g mouse on\n"
	if err := os.WriteFile(path, []byte(manual), 0600); err != nil {
		t.Fatal(err)
	}
	if err := configureTmux(account); err != nil {
		t.Fatal(err)
	}
	if err := Cleanup(account); err != nil {
		t.Fatal(err)
	}
	if content := shared.ReadFileString(path); content != manual || IsTmuxConfigured(account) {
		t.Fatalf("manual tmux settings changed: %q", content)
	}
	if err := Cleanup(account); err != nil {
		t.Fatal(err)
	}
}

func TestConfigureTmuxPreservesManualConfigAndIsIdempotent(t *testing.T) {
	manual := "set -g mouse on\nset -g prefix C-a\n"
	for _, existing := range []string{
		manual,
		manual + "\n" + shared.FormatManagedBlock(tmuxBegin, "set -g status off", tmuxEnd),
	} {
		account := &system.Account{Home: t.TempDir(), UID: os.Getuid(), GID: os.Getgid()}
		path := filepath.Join(account.Home, ".tmux.conf")
		if err := os.WriteFile(path, []byte(existing), 0600); err != nil {
			t.Fatal(err)
		}
		if err := configureTmux(account); err != nil {
			t.Fatal(err)
		}
		first := shared.ReadFileString(path)
		if !strings.HasPrefix(first, manual) || !IsTmuxConfigured(account) {
			t.Fatalf("manual configuration or current template missing:\n%s", first)
		}
		if strings.Contains(first, "set -g status off") || strings.Count(first, tmuxBegin) != 1 || strings.Count(first, tmuxEnd) != 1 {
			t.Fatalf("managed block was not replaced exactly once:\n%s", first)
		}
		if info, err := os.Stat(path); err != nil {
			t.Fatal(err)
		} else if info.Mode().Perm() != 0600 {
			t.Fatalf("file mode = %o, want 600", info.Mode().Perm())
		}
		if err := configureTmux(account); err != nil {
			t.Fatal(err)
		}
		if second := shared.ReadFileString(path); second != first {
			t.Fatalf("repeated configuration changed the file:\n%s", second)
		}
	}
}

func TestConfigureTmuxCreatesFileAndDetectsModifiedConfiguration(t *testing.T) {
	account := &system.Account{Home: t.TempDir(), UID: os.Getuid(), GID: os.Getgid()}
	if IsTmuxConfigured(account) {
		t.Fatal("missing file was reported as configured")
	}
	if err := configureTmux(account); err != nil {
		t.Fatal(err)
	}
	if !IsTmuxConfigured(account) {
		t.Fatal("new configuration was not detected")
	}
	path := filepath.Join(account.Home, ".tmux.conf")
	modified := strings.Replace(shared.ReadFileString(path), "status-position bottom", "status-position top", 1)
	if err := os.WriteFile(path, []byte(modified), 0644); err != nil {
		t.Fatal(err)
	}
	if IsTmuxConfigured(account) {
		t.Fatal("modified configuration was reported as configured")
	}
}
