package temps

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"snail_tool/internal/shared"
	"snail_tool/internal/tempguard"
	"snail_tool/internal/ui"
)

func TestMain(m *testing.M) {
	// Other packages exercise producers concurrently under go test ./....
	// Keep this package's cleanup lock isolated from those unrelated fixtures.
	dir, err := os.MkdirTemp("", "servertool-temps-tests-")
	if err != nil {
		panic(err)
	}
	temporaryFiles = tempguard.Guard{Path: filepath.Join(dir, "guard.lock")}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestCleanupRefusesActiveProducerWithAllPayloadFilesClosed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "servertool-action-1.sh")
	if err := os.WriteFile(path, []byte("needed by the next step"), 0600); err != nil {
		t.Fatal(err)
	}
	items, _, err := Scan(Locations{TempDirs: []string{dir}})
	if err != nil {
		t.Fatal(err)
	}
	release, err := temporaryFiles.Use()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, _, err := Scan(Locations{TempDirs: []string{dir}}); err == nil {
		t.Fatal("scan accepted an active producer without open payload files")
	}
	if err := Delete(items); err == nil {
		t.Fatal("delete accepted a producer started after confirmation")
	}
	if shared.ReadFileString(path) != "needed by the next step" {
		t.Fatal("active producer's closed payload was removed")
	}
}

func TestDeleteBlocksNewProducersUntilEntireBatchFinishes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "servertool-action-1.sh")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	items, _, err := Scan(Locations{TempDirs: []string{dir}})
	if err != nil {
		t.Fatal(err)
	}
	previous := beforeRemove
	beforeRemove = func(Item) {
		if release, err := temporaryFiles.Use(); err == nil {
			release()
			t.Fatal("producer started during deletion")
		}
	}
	t.Cleanup(func() { beforeRemove = previous })
	if err := Delete(items); err != nil {
		t.Fatal(err)
	}
	release, err := temporaryFiles.Use()
	if err != nil {
		t.Fatalf("cleanup did not release lock: %v", err)
	}
	release()
}

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

	items, busy, err := Scan(Locations{TempDirs: []string{tempDir, tempDir}, EtcDirs: []string{etcDir, filepath.Join(root, "missing")}, GoRoot: goRoot})
	if err != nil {
		t.Fatal(err)
	}
	got := pathsOf(items)
	want := append(append([]string{}, remove...), installDir, filepath.Join(goRoot, ".install-15"))
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") || len(busy) != 0 {
		t.Fatalf("scan = %v busy = %v, want %v", got, busy, want)
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
	if _, _, err := Scan(Locations{TempDirs: []string{tempDir}}); err == nil || !strings.Contains(err.Error(), "类型异常") {
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
	if _, _, err := Scan(Locations{EtcDirs: []string{etcLink}}); err == nil || !strings.Contains(err.Error(), "软链接") {
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
	items, _, err := Scan(Locations{TempDirs: []string{dir}})
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

func TestScanSkipsFilesHeldOpen(t *testing.T) {
	dir := t.TempDir()
	held := filepath.Join(dir, "servertool-action-1.sh")
	idle := filepath.Join(dir, "snail-sshd-2.conf")
	for _, path := range []string{held, idle} {
		if err := os.WriteFile(path, []byte("data"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	installDir := filepath.Join(dir, "servertool-install.Abc123")
	if err := os.Mkdir(installDir, 0700); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(installDir, "payload")
	if err := os.WriteFile(payload, []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	heldFile, err := os.Open(held)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = heldFile.Close() })
	payloadFile, err := os.Open(payload)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = payloadFile.Close() })

	items, busy, err := Scan(Locations{TempDirs: []string{dir}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(pathsOf(items), "\n") != idle {
		t.Fatalf("deletion candidates = %v", pathsOf(items))
	}
	sort.Strings(busy)
	wantBusy := []string{held, installDir}
	sort.Strings(wantBusy)
	if strings.Join(busy, "\n") != strings.Join(wantBusy, "\n") {
		t.Fatalf("busy = %v, want %v", busy, wantBusy)
	}
	if err := Delete(items); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{held, installDir, payload} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("in-use path removed: %s %v", path, err)
		}
	}
	if _, err := os.Lstat(idle); !os.IsNotExist(err) {
		t.Fatalf("idle file still present: %v", err)
	}
}

func TestDeleteRechecksIdentityImmediatelyBeforeRemoval(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "servertool-action-1.sh")
	second := filepath.Join(dir, "snail-sshd-2.conf")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	items, _, err := Scan(Locations{TempDirs: []string{dir}})
	if err != nil {
		t.Fatal(err)
	}
	previous := beforeRemove
	beforeRemove = func(item Item) {
		if item.Path != second {
			return
		}
		if err := os.Remove(second); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(second, []byte("replaced"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { beforeRemove = previous })
	if err := Delete(items); err == nil || !strings.Contains(err.Error(), "确认期间变化") {
		t.Fatalf("replacement deleted: %v", err)
	}
	if _, err := os.Lstat(first); !os.IsNotExist(err) {
		t.Fatalf("unchanged file still present: %v", err)
	}
	if shared.ReadFileString(second) != "replaced" {
		t.Fatalf("replacement content = %q", shared.ReadFileString(second))
	}
}

func TestDeleteStopsWhenAFileBecomesBusy(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "servertool-action-1.sh")
	second := filepath.Join(dir, "snail-sshd-2.conf")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	items, _, err := Scan(Locations{TempDirs: []string{dir}})
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	if err := Delete(items); err == nil || !strings.Contains(err.Error(), "仍被进程使用") {
		t.Fatalf("busy file accepted: %v", err)
	}
	for _, path := range []string{first, second} {
		if shared.ReadFileString(path) != "old" {
			t.Fatalf("file changed: %s %q", path, shared.ReadFileString(path))
		}
	}
}

func TestScanContinuesWhenUnrelatedTempFileDisappears(t *testing.T) {
	dir := t.TempDir()
	unrelated := filepath.Join(dir, "unrelated")
	target := filepath.Join(dir, "servertool-action-1.sh")
	for _, path := range []string{unrelated, target} {
		if err := os.WriteFile(path, []byte("data"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	previous := readDirectory
	readDirectory = func(name string) ([]os.DirEntry, error) {
		entries, err := os.ReadDir(name)
		if err != nil {
			return nil, err
		}
		if err := os.Remove(unrelated); err != nil {
			t.Fatal(err)
		}
		return entries, nil
	}
	t.Cleanup(func() { readDirectory = previous })
	items, _, err := Scan(Locations{TempDirs: []string{dir}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(pathsOf(items), "\n") != target {
		t.Fatalf("scan = %v", pathsOf(items))
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
