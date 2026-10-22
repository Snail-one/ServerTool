package tempguard

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProducersShareLockAndCleanupExcludesThem(t *testing.T) {
	guard := Guard{Path: filepath.Join(t.TempDir(), "guard.lock")}
	first, err := guard.Use()
	if err != nil {
		t.Fatal(err)
	}
	defer first()
	second, err := guard.Use()
	if err != nil {
		t.Fatal(err)
	}
	if release, err := guard.Cleanup(); err == nil {
		release()
		t.Fatal("cleanup accepted active producers")
	}
	second()
	if release, err := guard.Cleanup(); err == nil {
		release()
		t.Fatal("releasing a nested producer released the outer lock")
	}
	first()
	cleanup, err := guard.Cleanup()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if release, err := guard.Use(); err == nil {
		release()
		t.Fatal("producer started during cleanup")
	}
	if release, err := guard.Cleanup(); err == nil {
		release()
		t.Fatal("concurrent cleanup started")
	}
	cleanup()
	if _, err := os.Stat(guard.Path); err != nil {
		t.Fatal("lock inode must remain after release", err)
	}
}

func TestShellProducerProtectsClosedPayloadAndCrashReleasesLock(t *testing.T) {
	if _, err := exec.LookPath("flock"); err != nil {
		t.Skip("flock unavailable")
	}
	guard := Guard{Path: filepath.Join(t.TempDir(), "guard.lock")}
	// Initialize with the same private permissions used by the real installer.
	release, err := guard.Use()
	if err != nil {
		t.Fatal(err)
	}
	release()
	// Exercise the real installer function without running installation/network steps.
	script, err := os.ReadFile("../../scripts/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	prefix, _, found := strings.Cut(string(script), "trap cleanup 0")
	if !found {
		t.Fatal("installer function boundary missing")
	}
	cleanup, err := guard.Cleanup()
	if err != nil {
		t.Fatal(err)
	}
	blocked := exec.Command("sh", "-c", prefix+`lock_temporary_files "$1"`, "sh", guard.Path)
	output, blockedErr := blocked.CombinedOutput()
	cleanup()
	if blockedErr == nil || !strings.Contains(string(output), "正在清理中断临时文件") {
		t.Fatalf("installer started during Go cleanup: %q %v", output, blockedErr)
	}
	command := exec.Command("sh", "-c", prefix+`lock_temporary_files "$1"
printf 'ready\n'
read -r finish`, "sh", guard.Path)
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
	if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatalf("shell did not acquire lock: %q %v", line, err)
	}
	if release, err := guard.Cleanup(); err == nil {
		release()
		t.Fatal("cleanup ignored live shell without open payload files")
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()
	cleanup, err = guard.Cleanup()
	if err != nil {
		t.Fatalf("crashed producer left a stale lock: %v", err)
	}
	cleanup()
}

func TestRefusesSymlinkAndHardlinkedLock(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, link := range []func(string, string) error{os.Symlink, os.Link} {
		path := filepath.Join(dir, "guard.lock")
		if err := link(target, path); err != nil {
			t.Fatal(err)
		}
		if release, err := (Guard{Path: path}).Use(); err == nil {
			release()
			t.Fatal("linked lock accepted")
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "keep" {
		t.Fatalf("lock target changed: %q %v", data, err)
	}
}
