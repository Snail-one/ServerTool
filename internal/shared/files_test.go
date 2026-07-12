package shared

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFormatManagedBlockAddsBlankLinesAroundBody(t *testing.T) {
	got := FormatManagedBlock("# BEGIN", "line one\nline two", "# END")
	want := "# BEGIN\n\nline one\nline two\n\n# END\n"
	if got != want {
		t.Fatalf("FormatManagedBlock() = %q, want %q", got, want)
	}
}

func TestAtomicWriteFileReplacesAndPreservesMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte("old"), 0640); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := AtomicWriteFile(path, []byte("new"), AtomicWriteOptions{Mode: 0600}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "new" {
		t.Fatalf("content = %q", data)
	}
	after, _ := os.Stat(path)
	if after.Mode().Perm() != 0640 {
		t.Fatalf("mode = %o, want 640", after.Mode().Perm())
	}
	beforeOwner, _ := fileOwner(before)
	afterOwner, _ := fileOwner(after)
	if beforeOwner != nil && *beforeOwner != *afterOwner {
		t.Fatalf("owner changed from %+v to %+v", beforeOwner, afterOwner)
	}
}

func TestAtomicWriteFileNewAndForcedMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if err := AtomicWriteFile(path, []byte("one"), AtomicWriteOptions{Mode: 0640}); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0640 {
		t.Fatalf("new mode = %o", info.Mode().Perm())
	}
	if err := AtomicWriteFile(path, []byte("two"), AtomicWriteOptions{Mode: 0600, ForceMode: true}); err != nil {
		t.Fatal(err)
	}
	info, _ = os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("forced mode = %o", info.Mode().Perm())
	}
}

func TestAtomicWriteFileAppliesExplicitOwner(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file ownership is not available")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	owner := &FileOwner{UID: os.Getuid(), GID: os.Getgid()}
	if err := AtomicWriteFile(path, []byte("owned"), AtomicWriteOptions{Mode: 0600, Owner: owner}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := fileOwner(info)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || *got != *owner {
		t.Fatalf("owner = %+v, want %+v", got, owner)
	}
}

func TestAtomicWriteFailureRollsBackAndCleansTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	err := AtomicWrite(path, AtomicWriteOptions{Mode: 0644}, func(writer io.Writer) error {
		_, _ = io.WriteString(writer, "partial")
		return errors.New("injected write failure")
	})
	if err == nil || !strings.Contains(err.Error(), "injected write failure") {
		t.Fatalf("error = %v", err)
	}
	assertAtomicRollback(t, dir, path)
}

func TestAtomicSyncFailureRollsBackAndCleansTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	originalSync := syncAtomicFile
	syncAtomicFile = func(*os.File) error { return errors.New("injected sync failure") }
	t.Cleanup(func() { syncAtomicFile = originalSync })
	if err := AtomicWriteFile(path, []byte("replacement"), AtomicWriteOptions{Mode: 0644}); err == nil {
		t.Fatal("expected sync failure")
	}
	assertAtomicRollback(t, dir, path)
}

func TestAtomicWriteRejectsSymlinksAndNonRegularTargets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require privileges")
	}
	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	if err := os.Mkdir(realDir, 0755); err != nil {
		t.Fatal(err)
	}
	parentLink := filepath.Join(dir, "linked")
	if err := os.Symlink(realDir, parentLink); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteFile(filepath.Join(parentLink, "config"), []byte("x"), AtomicWriteOptions{Mode: 0644}); err == nil {
		t.Fatal("expected parent symlink rejection")
	}

	realFile := filepath.Join(realDir, "real-config")
	if err := os.WriteFile(realFile, []byte("safe"), 0644); err != nil {
		t.Fatal(err)
	}
	targetLink := filepath.Join(realDir, "config-link")
	if err := os.Symlink(realFile, targetLink); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteFile(targetLink, []byte("x"), AtomicWriteOptions{Mode: 0644}); err == nil {
		t.Fatal("expected target symlink rejection")
	}
	data, _ := os.ReadFile(realFile)
	if string(data) != "safe" {
		t.Fatalf("symlink destination changed: %q", data)
	}
	if err := AtomicWriteFile(realDir, []byte("x"), AtomicWriteOptions{Mode: 0644}); err == nil {
		t.Fatal("expected directory target rejection")
	}
}

func TestRemoveRegularFileRemovesFileAndRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require privileges")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte("remove"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := RemoveRegularFile(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("file still exists: %v", err)
	}

	realPath := filepath.Join(dir, "real")
	linkPath := filepath.Join(dir, "link")
	if err := os.WriteFile(realPath, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Fatal(err)
	}
	if err := RemoveRegularFile(linkPath); err == nil {
		t.Fatal("expected symlink rejection")
	}
	data, err := os.ReadFile(realPath)
	if err != nil || string(data) != "keep" {
		t.Fatalf("symlink destination changed: data=%q err=%v", data, err)
	}
}

func TestRemovePathTreeRemovesDirectoryAndRejectsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require privileges")
	}
	dir := t.TempDir()
	tree := filepath.Join(dir, "tree")
	if err := os.MkdirAll(filepath.Join(tree, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree, "nested", "data"), []byte("delete"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := RemovePathTree(tree); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(tree); !os.IsNotExist(err) {
		t.Fatalf("tree still exists: %v", err)
	}

	realDir := filepath.Join(dir, "real")
	if err := os.Mkdir(realDir, 0755); err != nil {
		t.Fatal(err)
	}
	targetLink := filepath.Join(dir, "target-link")
	if err := os.Symlink(realDir, targetLink); err != nil {
		t.Fatal(err)
	}
	if err := RemovePathTree(targetLink); err == nil {
		t.Fatal("expected target symlink rejection")
	}
	parentLink := filepath.Join(dir, "parent-link")
	if err := os.Symlink(realDir, parentLink); err != nil {
		t.Fatal(err)
	}
	if err := RemovePathTree(filepath.Join(parentLink, "child")); err == nil {
		t.Fatal("expected parent symlink rejection")
	}
	if _, err := os.Stat(realDir); err != nil {
		t.Fatalf("symlink destination was removed: %v", err)
	}
}

func assertAtomicRollback(t *testing.T, dir, path string) {
	t.Helper()
	data, _ := os.ReadFile(path)
	if string(data) != "original" {
		t.Fatalf("original changed: %q", data)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".servertool-atomic-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files remain: %v", matches)
	}
}
