package shared

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupRejectsDamagedMarkersWithoutChangingFile(t *testing.T) {
	for _, content := range []string{
		"# BEGIN\nmanual\n# BEGIN\nowned\n# END\n",
		"# BEGIN\nmanual\n", "# END\nmanual\n# BEGIN\n",
		"echo '# BEGIN'\nmanual\n# END\n",
		"# BEGIN\nowned\n# END\nmanual\n# END\n",
	} {
		path := filepath.Join(t.TempDir(), "config")
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := CleanupManagedBlocks(path, BlockMarker{Begin: "# BEGIN", End: "# END"}); err == nil {
			t.Fatal("ambiguous deletion accepted")
		}
		if ReadFileString(path) != content {
			t.Fatal("damaged markers caused file changes")
		}
	}
}

func TestAtomicWritePreservesConcurrentReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	err := AtomicWrite(path, AtomicWriteOptions{Mode: 0600}, func(w io.Writer) error {
		if err := os.WriteFile(path, []byte("external update"), 0600); err != nil {
			return err
		}
		_, err := io.WriteString(w, "stale tool content")
		return err
	})
	if err == nil || ReadFileString(path) != "external update" {
		t.Fatalf("external update overwritten: %v", err)
	}
}

func TestRecursiveRemovalRefusesBroadPathsAndMountedTrees(t *testing.T) {
	for _, path := range []string{"", ".", "/", "/etc", "/var/lib", "/usr/local/bin", "relative/path", "/tmp/../etc"} {
		if err := CheckRemovalTree(path); err == nil {
			t.Fatalf("unsafe path accepted: %q", path)
		}
	}
	for _, point := range []string{"/safe/data", "/safe/data/child", `/safe/data/a\040b`} {
		if err := checkRemovalMounts("/safe/data", "36 25 0:32 / "+point+" rw - tmpfs tmpfs rw\n"); err == nil {
			t.Fatalf("mount accepted: %s", point)
		}
	}
	if err := checkRemovalMounts("/safe/data", "36 25 0:32 / /safe/database rw - tmpfs tmpfs rw\n"); err != nil {
		t.Fatal("unrelated prefix rejected", err)
	}
	if err := checkRemovalMounts("/safe/data", "invalid"); err == nil {
		t.Fatal("invalid mount information accepted")
	}
}

func TestManagedWriteRecordRejectsManualChangesAndIsNotABackup(t *testing.T) {
	store := BackupStore{Root: filepath.Join(t.TempDir(), "snailtool"), Module: "docker"}
	source := filepath.Join(t.TempDir(), "config")
	data := []byte("written by tool")
	if ok, err := store.MatchesManaged(source, data); err != nil || ok {
		t.Fatalf("unrecorded file claimed: %v %v", ok, err)
	}
	if err := store.RememberManaged(source, data); err != nil {
		t.Fatal(err)
	}
	if ok, err := store.MatchesManaged(source, data); err != nil || !ok {
		t.Fatalf("record missing: %v %v", ok, err)
	}
	if ok, err := store.MatchesManaged(source, []byte("manual settings")); err != nil || ok {
		t.Fatalf("manual changes claimed: %v %v", ok, err)
	}
	if entries, err := store.List(); err != nil || len(entries) != 0 {
		t.Fatalf("ownership record treated as disposable backup: %v %v", entries, err)
	}
	if err := store.ForgetManaged(source); err != nil {
		t.Fatal(err)
	}
	if ok, err := store.MatchesManaged(source, data); err != nil || ok {
		t.Fatal("cleaned ownership still claims future identical manual config", err)
	}
}

func TestBackupCleanupDoesNotClaimArbitraryBakNames(t *testing.T) {
	for _, name := range []string{"notes.bak.manual", "config.bak.20261399_252525-123", "config.bak.1", ".hidden.bak.20260101_010101-123"} {
		if isBackupName(name) {
			t.Fatalf("unrelated filename accepted as backup: %s", name)
		}
	}
	if !isBackupName("config.bak.20260101_010101-123") {
		t.Fatal("valid tool snapshot not recognized")
	}
}
