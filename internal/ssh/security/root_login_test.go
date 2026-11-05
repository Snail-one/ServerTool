package security

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"snail_tool/internal/shared"
)

func TestRootLoginForHardeningPreservesRestrictions(t *testing.T) {
	for _, value := range []string{"no", "prohibit-password", "without-password", "forced-commands-only", "yes", ""} {
		t.Run(value, func(t *testing.T) {
			want := value
			if value == "yes" || value == "" {
				want = "prohibit-password"
			}
			if got := rootLoginForHardening(parseSSHDSettings("permitrootlogin " + value)); got != want {
				t.Fatalf("policy=%q, want %q (must not depend on target user)", got, want)
			}
		})
	}
}

func TestReplaceManagedRootLoginPreservesOtherConfiguration(t *testing.T) {
	content := "# local header\n" + buildSSHDConfig(3022, "prohibit-password") + "# local footer\nLogLevel VERBOSE\n"
	got, err := replaceManagedRootLogin(content, "no")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(content, "PermitRootLogin prohibit-password", "PermitRootLogin no", 1)
	if got != want {
		t.Fatalf("unrelated SSH configuration changed:\n%s", got)
	}
	if restored, err := replaceManagedRootLogin(got, "prohibit-password"); err != nil || restored != content {
		t.Fatalf("cannot restore root public-key login: %q, %v", restored, err)
	}
}

func TestReplaceManagedRootLoginRejectsAmbiguousConfiguration(t *testing.T) {
	for _, content := range []string{
		"PermitRootLogin yes\n",
		buildSSHDConfig(3022, "no") + buildSSHDConfig(22, "no"),
		managedSSHDConfigEnd + "\n" + managedSSHDConfigBegin,
		managedSSHDConfigBegin + "\nPort 3022\n" + managedSSHDConfigEnd,
		managedSSHDConfigBegin + "\nMatch User root\nPermitRootLogin no\n" + managedSSHDConfigEnd,
	} {
		if _, err := replaceManagedRootLogin(content, "no"); err == nil {
			t.Fatalf("accepted ambiguous configuration: %q", content)
		}
	}
	if _, err := replaceManagedRootLogin(buildSSHDConfig(3022, "no"), "yes"); err == nil {
		t.Fatal("must not allow root password login")
	}
}

func TestUpdateRootLoginPolicyValidatesAndRestoresOnFailure(t *testing.T) {
	oldValidate, oldCheck := validateRootLoginConfig, checkRootLoginConfig
	oldRead, oldReload, oldBackup := readSettingsForReload, reloadConfiguredSSH, backupStore
	t.Cleanup(func() {
		validateRootLoginConfig, checkRootLoginConfig = oldValidate, oldCheck
		readSettingsForReload, reloadConfiguredSSH, backupStore = oldRead, oldReload, oldBackup
	})
	for _, failure := range []string{"", "candidate", "installed", "read", "overridden", "reload"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "99-custom.conf")
			original := buildSSHDConfig(3022, "prohibit-password")
			if err := os.WriteFile(path, []byte(original), 0644); err != nil {
				t.Fatal(err)
			}
			backupStore = shared.BackupStore{Root: filepath.Join(dir, "backups"), Module: "ssh"}
			validateRootLoginConfig = func(content string) error {
				if shared.ReadFileString(path) != original {
					t.Fatal("file modified before candidate validation")
				}
				if !strings.Contains(content, "PermitRootLogin no") {
					t.Fatal("candidate does not disable root")
				}
				if failure == "candidate" {
					return errors.New("candidate failed")
				}
				return nil
			}
			checkRootLoginConfig = func() error {
				if failure == "installed" {
					return errors.New("installed config failed")
				}
				return nil
			}
			readSettingsForReload = func() (sshdSettings, string, error) {
				if failure == "read" {
					return nil, "", errors.New("sshd failed")
				}
				if failure == "overridden" {
					return parseSSHDSettings("permitrootlogin prohibit-password"), "sshd -T", nil
				}
				return parseSSHDSettings("permitrootlogin no"), "sshd -T", nil
			}
			reloads := 0
			reloadConfiguredSSH = func() error {
				reloads++
				if failure == "reload" && reloads == 1 {
					return errors.New("reload failed")
				}
				if reloads == 2 && shared.ReadFileString(path) != original {
					t.Fatal("original config not restored before retrying reload")
				}
				return nil
			}
			err := updateRootLoginPolicy(path, "no")
			want := original
			wantReloads := 0
			if failure == "" {
				if err != nil {
					t.Fatal(err)
				}
				want = strings.Replace(original, "PermitRootLogin prohibit-password", "PermitRootLogin no", 1)
				wantReloads = 1
			} else if err == nil {
				t.Fatal("failure ignored")
			}
			if failure == "reload" {
				wantReloads = 2
			}
			if got := shared.ReadFileString(path); got != want {
				t.Fatalf("configuration after operation=%q, want %q", got, want)
			}
			if reloads != wantReloads {
				t.Fatalf("reloads=%d, want %d", reloads, wantReloads)
			}
		})
	}
}
