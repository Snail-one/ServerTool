package security

import (
	"os"
	"path/filepath"
	"testing"

	"snail_tool/internal/storage"
)

func TestCleanupSSHDKeepsManualContentAndPreflightsBothFiles(t *testing.T) {
	for _, damaged := range []bool{false, true} {
		t.Run(map[bool]string{false: "manual content", true: "damaged custom block"}[damaged], func(t *testing.T) {
			dir := t.TempDir()
			sshdPath, customPath := filepath.Join(dir, "sshd_config"), filepath.Join(dir, "custom.conf")
			include := storage.FormatManagedBlock(managedSSHDIncludeBegin, "Include custom.conf", managedSSHDIncludeEnd)
			begin, end := managedSSHDConfigBegin, managedSSHDConfigEnd
			custom := storage.FormatManagedBlock(begin, "PasswordAuthentication no", end) + "AllowUsers manual-user\n"
			if damaged {
				custom += begin + "\n"
			}
			for path, content := range map[string]string{sshdPath: include, customPath: custom} {
				if err := os.WriteFile(path, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			changed, err := cleanupSSHDConfig(sshdPath, customPath)
			if damaged {
				if err == nil || changed || storage.ReadFileString(sshdPath) != include || storage.ReadFileString(customPath) != custom {
					t.Fatal("damaged custom config caused partial cleanup", err)
				}
			} else if err != nil || !changed || storage.ReadFileString(customPath) != "AllowUsers manual-user\n" {
				t.Fatal("manual SSH directives removed", err)
			}
		})
	}
}
