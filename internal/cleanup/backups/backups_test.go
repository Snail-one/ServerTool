package backups

import (
	"os"
	"path/filepath"
	"testing"

	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

func setup(t *testing.T) {
	t.Helper()
	previous := dataDir
	dataDir = filepath.Join(t.TempDir(), "snailtool")
	t.Cleanup(func() { dataDir = previous })
}

func testView(t *testing.T, input string) *ui.UI {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.WriteString(input); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	previous := os.Stdin
	os.Stdin = reader
	view := ui.New()
	os.Stdin = previous
	t.Cleanup(func() { _ = reader.Close() })
	return view
}

func seed(t *testing.T, name string) string {
	t.Helper()
	store := shared.BackupStore{Root: dataDir, Module: name}
	source := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(source, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	var backup string
	var err error
	if name == "ups" {
		backup, _, err = store.KeepOriginal(source)
	} else {
		backup, err = store.Snapshot(source)
	}
	if err != nil {
		t.Fatal(err)
	}
	return backup
}

func TestBackupCleanupConfirmationAndCancellation(t *testing.T) {
	for _, input := range []string{"y\n", "n\n", "\n", ""} {
		t.Run(input, func(t *testing.T) {
			setup(t)
			backup := seed(t, "firewall")
			if err := ConfirmAll(testView(t, input)); err != nil {
				t.Fatal(err)
			}
			_, err := os.Stat(backup)
			if input == "y\n" {
				if !os.IsNotExist(err) {
					t.Fatal("confirmed backup was not deleted")
				}
			} else if err != nil {
				t.Fatalf("unconfirmed cleanup deleted a backup: %v", err)
			}
		})
	}
}

func TestModuleCleanupAndUninstallDeleteAll(t *testing.T) {
	setup(t)
	var paths []string
	for _, item := range modules {
		paths = append(paths, seed(t, item.directory))
	}
	state := filepath.Join(dataDir, "state.json")
	if err := os.WriteFile(state, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := prepare(modules[:1])
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Delete(); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths[1:] {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("other module deleted: %s %v", path, err)
		}
	}
	if err := DeleteAll(); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("backup remains after uninstall cleanup: %s %v", path, err)
		}
	}
	if shared.ReadFileString(state) != "keep" {
		t.Fatal("uninstall cleanup touched other persistent data")
	}
}

func TestNoBackupsDoesNotCreateDataDirectory(t *testing.T) {
	setup(t)
	if err := DeleteAll(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatalf("empty cleanup created data directory: %v", err)
	}
}
