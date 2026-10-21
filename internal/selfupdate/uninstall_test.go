package selfupdate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUninstallScriptCleansBackupsBeforeRemovingProgram(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "backup cleanup failure"}[failed], func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "snail")
			marker := filepath.Join(dir, "cleanup-called")
			stub := `#!/bin/sh
case "$1" in
  --version) printf 'snailtool test\n' ;;
  clean-backups)
    [ "$2" = "--yes" ] || exit 7
    [ -f "$0" ] || exit 8
    printf 'called' > "$TEST_BACKUP_MARKER"
    exit "$TEST_BACKUP_EXIT"
    ;;
  *) exit 9 ;;
esac
`
			if err := os.WriteFile(target, []byte(stub), 0700); err != nil {
				t.Fatal(err)
			}
			// This test never needs root or a real installed program.
			if err := os.WriteFile(filepath.Join(dir, "id"), []byte("#!/bin/sh\nprintf '0\\n'\n"), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("SERVERTOOL_INSTALL_DIR", dir)
			t.Setenv("SERVERTOOL_BINARY_NAME", "snail")
			t.Setenv("TEST_BACKUP_MARKER", marker)
			status := "0"
			if failed {
				status = "5"
			}
			t.Setenv("TEST_BACKUP_EXIT", status)
			command := exec.Command("sh", "../../scripts/install.sh", "--uninstall")
			command.Stdin = strings.NewReader("")
			output, err := command.CombinedOutput()
			if (err != nil) != failed {
				t.Fatalf("uninstall err=%v output=%s", err, output)
			}
			if data, err := os.ReadFile(marker); err != nil || string(data) != "called" {
				t.Fatalf("default uninstall did not clean backups: %s %v", data, err)
			}
			_, err = os.Stat(target)
			if failed && err != nil {
				t.Fatalf("cleanup failure must retain program: %v", err)
			}
			if !failed && !os.IsNotExist(err) {
				t.Fatalf("program was not removed after backup cleanup: %v", err)
			}
		})
	}
}
