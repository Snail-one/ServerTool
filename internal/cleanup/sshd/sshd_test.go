package sshd

import (
	"os"
	"path/filepath"
	"testing"

	"snail_tool/internal/shared"
	"snail_tool/internal/ssh/security"
)

func TestCleanupSSHDKeepsManualContentAndPreflightsBothFiles(t *testing.T) {
	for _, damaged := range []bool{false, true} {
		t.Run(map[bool]string{false: "manual content", true: "damaged custom block"}[damaged], func(t *testing.T) {
			oldConfig, oldCustom := sshdConfigPath, customSSHDConfigPath
			t.Cleanup(func() { sshdConfigPath, customSSHDConfigPath = oldConfig, oldCustom })
			dir := t.TempDir()
			sshdConfigPath, customSSHDConfigPath = filepath.Join(dir, "sshd_config"), filepath.Join(dir, "custom.conf")
			include := shared.FormatManagedBlock(managedSSHDIncludeBegin, "Include custom.conf", managedSSHDIncludeEnd)
			begin, end := security.ManagedSSHDConfigMarkers()
			custom := shared.FormatManagedBlock(begin, "PasswordAuthentication no", end) + "AllowUsers manual-user\n"
			if damaged {
				custom += begin + "\n"
			}
			for path, content := range map[string]string{sshdConfigPath: include, customSSHDConfigPath: custom} {
				if err := os.WriteFile(path, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			changed, err := cleanupSSHDConfig()
			if damaged {
				if err == nil || changed || shared.ReadFileString(sshdConfigPath) != include || shared.ReadFileString(customSSHDConfigPath) != custom {
					t.Fatal("damaged custom config caused partial cleanup", err)
				}
			} else if err != nil || !changed || shared.ReadFileString(customSSHDConfigPath) != "AllowUsers manual-user\n" {
				t.Fatal("manual SSH directives removed", err)
			}
		})
	}
}
