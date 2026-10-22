package selfupdate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUninstallScriptHonorsDataChoiceBeforeRemovingProgram(t *testing.T) {
	for _, scenario := range []struct {
		name, input                       string
		deleteData, failDelete, cancelled bool
	}{
		{name: "delete", input: "y\n", deleteData: true},
		{name: "keep", input: "n\n"},
		{name: "default keeps", input: "\n"},
		{name: "EOF keeps"},
		{name: "partial answer keeps", input: "y"},
		{name: "case insensitive yes", input: "YES\n", deleteData: true},
		{name: "invalid then delete", input: "invalid\ny\n", deleteData: true},
		{name: "invalid then keep", input: "invalid\nn\n"},
		{name: "cancel", input: "q\n", cancelled: true},
		{name: "cancel zero", input: "0\n", cancelled: true},
		{name: "deletion failure", input: "y\n", deleteData: true, failDelete: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "snail")
			marker := filepath.Join(dir, "cleanup-called")
			dataRoot := filepath.Join(dir, "snailtool")
			for _, name := range []string{"backups/module/config.bak.1", "backups/module/.snailtool-backup.lock", ".hidden", "state/other.json"} {
				path := filepath.Join(dataRoot, name)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("keep until yes"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			outside := filepath.Join(dir, "system-config")
			if err := os.WriteFile(outside, []byte("preserve config"), 0600); err != nil {
				t.Fatal(err)
			}
			stub := `#!/bin/sh
case "$1" in
  --version) printf 'snailtool test\n' ;;
  clean-data)
    [ "$2" = "--yes" ] || exit 7
    [ -f "$0" ] || exit 8
    printf 'called' > "$TEST_DATA_MARKER"
    [ "$TEST_DATA_EXIT" = 0 ] || exit "$TEST_DATA_EXIT"
    rm -rf -- "$TEST_DATA_ROOT"
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
			t.Setenv("TEST_DATA_MARKER", marker)
			t.Setenv("TEST_DATA_ROOT", dataRoot)
			status := "0"
			if scenario.failDelete {
				status = "5"
			}
			t.Setenv("TEST_DATA_EXIT", status)
			command := exec.Command("sh", "../../scripts/install.sh", "--uninstall")
			command.Stdin = strings.NewReader(scenario.input)
			output, err := command.CombinedOutput()
			if (err != nil) != scenario.failDelete {
				t.Fatalf("uninstall err=%v output=%s", err, output)
			}
			data, err := os.ReadFile(marker)
			if scenario.deleteData {
				if err != nil || string(data) != "called" {
					t.Fatalf("explicit deletion was not called: %s %v", data, err)
				}
			} else if !os.IsNotExist(err) {
				t.Fatal("deletion invoked without consent")
			}
			_, err = os.Stat(target)
			if (scenario.failDelete || scenario.cancelled) && err != nil {
				t.Fatalf("failure/cancellation must retain program: %v", err)
			}
			if !scenario.failDelete && !scenario.cancelled && !os.IsNotExist(err) {
				t.Fatalf("program was not removed after choosing data handling: %v", err)
			}
			_, err = os.Stat(dataRoot)
			if scenario.deleteData && !scenario.failDelete {
				if !os.IsNotExist(err) || !strings.Contains(string(output), "已清理 /var/lib/snailtool") {
					t.Fatalf("entire directory not removed or result wrong: %v %s", err, output)
				}
			} else if err != nil {
				t.Fatalf("data not retained: %v", err)
			}
			if !scenario.deleteData && !scenario.cancelled && !strings.Contains(string(output), "已保留 /var/lib/snailtool") {
				t.Fatalf("completion does not report retained data: %s", output)
			}
			if content, err := os.ReadFile(outside); err != nil || string(content) != "preserve config" {
				t.Fatalf("uninstall changed system configuration: %s %v", content, err)
			}
		})
	}
}
