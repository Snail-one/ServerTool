package temps

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

func TestScanKeepsOnlyRecognizedInterruptFiles(t *testing.T) {
	root := t.TempDir()
	tempDir := filepath.Join(root, "tmp")
	etcDir := filepath.Join(root, "etc", "ssh")
	goRoot := filepath.Join(root, "opt", "go")
	for _, dir := range []string{tempDir, etcDir, goRoot} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	keep := []string{
		filepath.Join(tempDir, "servertool-action-notes.sh"),
		filepath.Join(tempDir, "servertool-install.ABCDEFG"),
		filepath.Join(tempDir, "unrelated"),
		filepath.Join(etcDir, "sshd_config"),
		filepath.Join(etcDir, ".servertool-atomic-"),
		filepath.Join(goRoot, "go1.24.1", "bin", "go"),
	}
	remove := []string{
		filepath.Join(tempDir, "servertool-action-42.sh"),
		filepath.Join(tempDir, "servertool-docker-gpg-7"),
		filepath.Join(tempDir, "servertool-get-docker-8.sh"),
		filepath.Join(tempDir, "snail-ssh-key-9.pub"),
		filepath.Join(tempDir, "snail-sshd-10.conf"),
		filepath.Join(etcDir, ".servertool-atomic-11"),
		filepath.Join(etcDir, ".snail-nftables-12.conf"),
		filepath.Join(etcDir, ".snail-ssh-ports-13.nft"),
		filepath.Join(goRoot, ".download-14.tar.gz"),
	}
	installDir := filepath.Join(tempDir, "servertool-install.Abc123")
	if err := os.Mkdir(installDir, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside")
	if err := os.WriteFile(outside, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(installDir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "payload"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(goRoot, ".install-15"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range append(keep, remove...) {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("data"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	items, err := Scan(Locations{TempDirs: []string{tempDir, tempDir}, EtcDirs: []string{etcDir, filepath.Join(root, "missing")}, GoRoot: goRoot})
	if err != nil {
		t.Fatal(err)
	}
	got := pathsOf(items)
	want := append(append([]string{}, remove...), installDir, filepath.Join(goRoot, ".install-15"))
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("scan = %v, want %v", got, want)
	}

	if err := Delete(items); err != nil {
		t.Fatal(err)
	}
	for _, path := range append(keep, outside) {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("kept file missing: %s %v", path, err)
		}
	}
	for _, path := range append(remove, installDir, filepath.Join(goRoot, ".install-15")) {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("leftover still present: %s %v", path, err)
		}
	}
}

func TestScanStopsOnUnexpectedTypeOrLinkedDirectory(t *testing.T) {
	root := t.TempDir()
	tempDir := filepath.Join(root, "tmp")
	if err := os.Mkdir(tempDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "elsewhere"), filepath.Join(tempDir, "servertool-action-1.sh")); err != nil {
		t.Fatal(err)
	}
	if _, err := Scan(Locations{TempDirs: []string{tempDir}}); err == nil || !strings.Contains(err.Error(), "类型异常") {
		t.Fatalf("symlink file accepted: %v", err)
	}

	etcTarget := filepath.Join(root, "real-etc")
	if err := os.Mkdir(etcTarget, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(etcTarget, ".servertool-atomic-1"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	etcLink := filepath.Join(root, "etc-link")
	if err := os.Symlink(etcTarget, etcLink); err != nil {
		t.Fatal(err)
	}
	if _, err := Scan(Locations{EtcDirs: []string{etcLink}}); err == nil || !strings.Contains(err.Error(), "软链接") {
		t.Fatalf("linked directory accepted: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(etcTarget, ".servertool-atomic-1")); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteStopsWhenAListedFileChanges(t *testing.T) {
	dir := t.TempDir()
	changed := filepath.Join(dir, "servertool-action-1.sh")
	other := filepath.Join(dir, "snail-sshd-2.conf")
	for _, path := range []string{changed, other} {
		if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	items, err := Scan(Locations{TempDirs: []string{dir}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(changed); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(changed, []byte("replaced"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Delete(items); err == nil || !strings.Contains(err.Error(), "确认期间变化") {
		t.Fatalf("changed file deleted: %v", err)
	}
	for _, path := range []string{changed, other} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("file removed after failed delete: %s %v", path, err)
		}
	}
	if shared.ReadFileString(changed) != "replaced" {
		t.Fatalf("changed file content = %q", shared.ReadFileString(changed))
	}
}

func TestRunCancelKeepsFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "servertool-docker-gpg-3")
	if err := os.WriteFile(path, []byte("key"), 0600); err != nil {
		t.Fatal(err)
	}
	view := confirmView(t, "n\n")
	err := run(view, Locations{TempDirs: []string{dir}})
	if !errors.Is(err, shared.ErrReturnToMenu) {
		t.Fatalf("cancel = %v", err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatal(err)
	}
}

func confirmView(t *testing.T, input string) *ui.UI {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	if _, err := writer.WriteString(input); err != nil {
		_ = writer.Close()
		t.Fatal(err)
	}
	_ = writer.Close()
	previous := os.Stdin
	os.Stdin = reader
	t.Cleanup(func() { os.Stdin = previous })
	return ui.New()
}

func pathsOf(items []Item) []string {
	paths := make([]string, len(items))
	for i, item := range items {
		paths[i] = item.Path
	}
	return paths
}
