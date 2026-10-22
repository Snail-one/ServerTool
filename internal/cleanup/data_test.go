package cleanup

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

func softwareDataView(t *testing.T, input string) *ui.UI {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "input-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	if _, err := file.WriteString(input); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	previous := os.Stdin
	os.Stdin = file
	view := ui.New()
	os.Stdin = previous
	return view
}

func seedSoftwareData(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "snailtool")
	for _, name := range []string{"backups/firewall/etc/config.bak.1", "backups/firewall/etc/.snailtool-backup.lock", "backups/ups/config.original.json", ".hidden", "other/state.json"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("keep until confirmed"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "empty", "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestSoftwareDataDeletionRemovesEntireDirectoryAndPreservesOutsideFiles(t *testing.T) {
	root := seedSoftwareData(t)
	outside := filepath.Join(t.TempDir(), "configuration")
	if err := os.WriteFile(outside, []byte("outside configuration"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(root, "external-link")); err != nil {
		t.Fatal(err)
	}
	if err := cleanupSoftwareData(softwareDataView(t, "y\n"), root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("entire data directory must be removed: %v", err)
	}
	if got := shared.ReadFileString(outside); got != "outside configuration" {
		t.Fatalf("deletion followed an external symlink: %q", got)
	}
	if err := cleanupSoftwareData(softwareDataView(t, ""), root); err != nil {
		t.Fatalf("deleting an absent directory must be harmless: %v", err)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatal("checking an absent directory recreated it")
	}
}

func TestSoftwareDataDeletionRequiresConfirmationWithoutAnotherPause(t *testing.T) {
	for _, answer := range []string{"n", "", "0", "q", "exit", "返回"} {
		t.Run("answer="+answer, func(t *testing.T) {
			root := seedSoftwareData(t)
			view := softwareDataView(t, answer+"\nnext\n")
			err := cleanupSoftwareData(view, root)
			if !errors.Is(err, shared.ErrReturnToMenu) {
				t.Fatalf("cancellation must return without pausing: %v", err)
			}
			if got := shared.ReadFileString(filepath.Join(root, ".hidden")); got != "keep until confirmed" {
				t.Fatal("cancelled deletion changed the directory")
			}
			if next, err := view.Ask("下一项："); err != nil || next != "next" {
				t.Fatalf("cancellation consumed next input: %q %v", next, err)
			}
		})
	}
	t.Run("EOF", func(t *testing.T) {
		root := seedSoftwareData(t)
		if err := cleanupSoftwareData(softwareDataView(t, ""), root); err == nil {
			t.Fatal("missing confirmation must not be treated as success")
		}
		if _, err := os.Stat(root); err != nil {
			t.Fatal("EOF removed the data directory")
		}
	})
}

func TestSoftwareDataDeletionRefusesActiveBackupLock(t *testing.T) {
	root := seedSoftwareData(t)
	lock, err := os.OpenFile(filepath.Join(root, "backups/firewall/etc/.snailtool-backup.lock"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if err := cleanupSoftwareData(softwareDataView(t, "y\n"), root); err == nil {
		t.Fatal("must not delete a directory used by an active backup")
	}
	if got := shared.ReadFileString(filepath.Join(root, ".hidden")); got != "keep until confirmed" {
		t.Fatal("active backup failure partially deleted the directory")
	}
}

func TestSoftwareDataDeletionRejectsRedirectedOrInvalidRoots(t *testing.T) {
	root := seedSoftwareData(t)
	linkParent := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(filepath.Dir(root), linkParent); err != nil {
		t.Fatal(err)
	}
	rootLink := filepath.Join(t.TempDir(), "snailtool")
	if err := os.Symlink(root, rootLink); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/var/lib", "snailtool", rootLink, filepath.Join(linkParent, "snailtool")} {
		if err := cleanupSoftwareData(softwareDataView(t, "y\n"), path); err == nil {
			t.Fatalf("invalid or redirected root accepted: %s", path)
		}
	}
	if got := shared.ReadFileString(filepath.Join(root, ".hidden")); got != "keep until confirmed" {
		t.Fatal("redirected deletion modified the real directory")
	}
}

func TestSoftwareDataDeletionRejectsReplacedDirectory(t *testing.T) {
	root := seedSoftwareData(t)
	info, err := inspectSoftwareData(root)
	if err != nil {
		t.Fatal(err)
	}
	saved := root + "-saved"
	if err := os.Rename(root, saved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "new-data")
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := deleteSoftwareData(root, info); err == nil {
		t.Fatal("replacement directory deleted")
	}
	if shared.ReadFileString(path) != "keep" || shared.ReadFileString(filepath.Join(saved, ".hidden")) != "keep until confirmed" {
		t.Fatal("original or replacement data changed")
	}
}
