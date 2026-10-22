package selfupdate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// scriptCommand 按 CI 的方式启动安装脚本：NO_COLOR 未设置、TERM=dumb、标准输出不是终端。
func scriptCommand(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	t.Setenv("NO_COLOR", "")
	os.Unsetenv("NO_COLOR")
	t.Setenv("FORCE_COLOR", "0")
	t.Setenv("TERM", "dumb")
	return exec.Command("sh", args...)
}

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
			command := scriptCommand(t, "../../scripts/install.sh", "--uninstall")
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

func TestUninstallRejectsUnknownOrRedirectedProgram(t *testing.T) {
	for _, scenario := range []string{"unknown program", "symlink target", "symlink parent", "dot filename"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			id := filepath.Join(dir, "id")
			if err := os.WriteFile(id, []byte("#!/bin/sh\nprintf '0\\n'\n"), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			binary := filepath.Join(dir, "snail")
			content := "#!/bin/sh\nprintf 'unrelated program\\n'\n"
			if scenario != "unknown program" {
				content = "#!/bin/sh\nprintf 'snailtool test\\n'\n"
			}
			if err := os.WriteFile(binary, []byte(content), 0700); err != nil {
				t.Fatal(err)
			}
			installDir, name := dir, "snail"
			if scenario == "symlink target" {
				name = "linked"
				if err := os.Symlink(binary, filepath.Join(dir, name)); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "symlink parent" {
				installDir = filepath.Join(t.TempDir(), "linked")
				if err := os.Symlink(dir, installDir); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "dot filename" {
				name = ".."
			}
			t.Setenv("SERVERTOOL_INSTALL_DIR", installDir)
			t.Setenv("SERVERTOOL_BINARY_NAME", name)
			command := scriptCommand(t, "../../scripts/install.sh", "--uninstall")
			command.Stdin = strings.NewReader("y\n")
			if out, err := command.CombinedOutput(); err == nil {
				t.Fatalf("ambiguous program accepted: %s", out)
			}
			if data, err := os.ReadFile(binary); err != nil || string(data) != content {
				t.Fatal("unrelated or redirected program changed", err)
			}
		})
	}
}

func TestUninstallPreservesProgramReplacedDuringDataDeletion(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "snail")
	stub := `#!/bin/sh
case "$1" in
--version) printf 'snailtool test\n';;
clean-data)
  printf 'replacement program\n' > "$0.replacement"
  mv "$0.replacement" "$0"
  ;;
*) exit 2;;
esac
`
	if err := os.WriteFile(target, []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "id"), []byte("#!/bin/sh\nprintf '0\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SERVERTOOL_INSTALL_DIR", dir)
	t.Setenv("SERVERTOOL_BINARY_NAME", "snail")
	command := scriptCommand(t, "../../scripts/install.sh", "--uninstall")
	command.Stdin = strings.NewReader("y\n")
	if out, err := command.CombinedOutput(); err == nil {
		t.Fatalf("replacement program deleted: %s", out)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "replacement program\n" {
		t.Fatal("replacement program was removed", err)
	}
}

func TestUninstallRejectsDataDirectoryReplacedDuringConfirmation(t *testing.T) {
	dir := t.TempDir()
	dataRoot := filepath.Join(dir, "snailtool")
	if err := os.Mkdir(dataRoot, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "snail")
	if err := os.WriteFile(target, []byte("#!/bin/sh\ncase \"$1\" in\n--version) printf 'snailtool test\\n';;\n*) printf 'unexpected deletion' > \"$TEST_DELETION_MARKER\";;\nesac\n"), 0700); err != nil {
		t.Fatal(err)
	}
	realStat, err := exec.LookPath("stat")
	if err != nil {
		t.Fatal(err)
	}
	statStub := `#!/bin/sh
if [ "$3" = "$TEST_DATA_ROOT" ]; then
  if [ -f "$TEST_STAT_SEEN" ]; then
    mv "$TEST_DATA_ROOT" "$TEST_DATA_ROOT.saved"
    mkdir "$TEST_DATA_ROOT"
  fi
  printf seen > "$TEST_STAT_SEEN"
fi
exec "$TEST_REAL_STAT" "$@"
`
	for name, body := range map[string]string{"stat": statStub, "id": "#!/bin/sh\nprintf '0\\n'\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0700); err != nil {
			t.Fatal(err)
		}
	}
	script, err := os.ReadFile("../../scripts/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	// Isolate the production script's fixed data path; no real system directory
	// is inspected or changed by this replacement-race fixture.
	scriptPath := filepath.Join(dir, "installer.sh")
	if err := os.WriteFile(scriptPath, []byte(strings.ReplaceAll(string(script), "/var/lib/snailtool", dataRoot)), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SERVERTOOL_INSTALL_DIR", dir)
	t.Setenv("SERVERTOOL_BINARY_NAME", "snail")
	t.Setenv("TEST_DATA_ROOT", dataRoot)
	t.Setenv("TEST_STAT_SEEN", filepath.Join(dir, "stat-seen"))
	t.Setenv("TEST_REAL_STAT", realStat)
	marker := filepath.Join(dir, "deletion-called")
	t.Setenv("TEST_DELETION_MARKER", marker)
	command := scriptCommand(t, scriptPath, "--uninstall")
	command.Stdin = strings.NewReader("y\n")
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "软件数据目录在确认期间已变化") {
		t.Fatalf("replacement directory accepted: %v %s", err, output)
	}
	for _, path := range []string{target, dataRoot, dataRoot + ".saved"} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("program or data removed: %s %v", path, err)
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("data deletion invoked after directory replacement")
	}
}
