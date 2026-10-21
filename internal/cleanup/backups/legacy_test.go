package backups

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"snail_tool/internal/shared"
)

func setupLegacy(t *testing.T) (string, []string) {
	t.Helper()
	setup(t)
	previous := legacyStore
	root := t.TempDir()
	legacyStore = shared.LegacyBackupStore{Root: root}
	t.Cleanup(func() { legacyStore = previous })
	var paths []string
	for _, name := range []string{"etc/nftables.conf.bak.20261021_051605-123", "etc/nut/ups.conf.bak"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("old backup"), 0600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	return root, paths
}

func captureOutput(t *testing.T, run func()) string {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "output-*")
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = file
	defer func() { os.Stdout = previous; _ = file.Close() }()
	run()
	data, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestLegacyCleanupListsEveryFileBeforeConfirmation(t *testing.T) {
	_, paths := setupLegacy(t)
	output := captureOutput(t, func() {
		if err := cleanLegacy(testView(t, "n\n")); err != nil {
			t.Fatal(err)
		}
	})
	confirmation := strings.Index(output, "确认永久删除清单中的全部旧备份？")
	for _, path := range paths {
		index := strings.Index(output, path)
		if index < 0 || confirmation < index {
			t.Fatalf("file was not listed before asking for confirmation: %s\n%s", path, output)
		}
		if shared.ReadFileString(path) != "old backup" {
			t.Fatal("cancelled cleanup changed a file")
		}
	}
	if !strings.Contains(output, "修改于") || !strings.Contains(output, "2 个文件") || !strings.Contains(output, "B") {
		t.Fatalf("inventory details missing: %s", output)
	}
}

func TestLegacyCleanupRequiresExplicitConfirmation(t *testing.T) {
	for _, input := range []string{"y\n", "\n", "", "n\n"} {
		t.Run(input, func(t *testing.T) {
			_, paths := setupLegacy(t)
			newBackup := seed(t, "firewall")
			if err := cleanLegacy(testView(t, input)); err != nil {
				t.Fatal(err)
			}
			for _, path := range paths {
				_, err := os.Stat(path)
				if input == "y\n" {
					if !os.IsNotExist(err) {
						t.Fatalf("confirmed file was not deleted: %s %v", path, err)
					}
				} else if err != nil {
					t.Fatalf("unconfirmed file was deleted: %s %v", path, err)
				}
			}
			if _, err := os.Stat(newBackup); err != nil {
				t.Fatal("legacy cleanup affected current backups")
			}
		})
	}
}

func TestLegacyCleanupIsAvailableFromBackupMenu(t *testing.T) {
	_, paths := setupLegacy(t)
	if err := Run(testView(t, "6\ny\n\nq\n")); !errors.Is(err, shared.ErrReturnToMenu) {
		t.Fatal(err)
	}
	for _, path := range paths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("menu did not delete confirmed legacy backup: %s %v", path, err)
		}
	}
}
