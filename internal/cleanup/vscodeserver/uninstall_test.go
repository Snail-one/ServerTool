package vscodeserver

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestInstallationVerificationRejectsReplacement(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".vscode-server")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	targets, err := inspectInstallations(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := verifyInstallations(targets); err == nil {
		t.Fatal("accepted replacement directory")
	}
}

func TestInstallationInspectionRejectsSymlink(t *testing.T) {
	home := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(home, ".vscode-server-insiders")); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectInstallations(home); err == nil {
		t.Fatal("accepted redirected installation directory")
	}
}

func TestStopOnlySignalsExecutablesFromTheTargetInstallation(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), ".vscode-server")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	start := func(path string) *exec.Cmd {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0700); err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(ctx, path, "-test.run=^TestServerProcessHelper$")
		cmd.Env = append(os.Environ(), "SERVERTOOL_TEST_VSCODE_SERVER=1")
		cmd.Stderr = os.Stderr
		ready, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		})
		// Wait until the child is running from the copied executable before scanning /proc.
		var started [1]byte
		if _, err := io.ReadFull(ready, started[:]); err != nil {
			t.Fatalf("waiting for %s to start: %v", path, err)
		}
		return cmd
	}
	server := start(filepath.Join(root, "code-server"))
	unrelated := start(filepath.Join(root+"-other", "code-server"))
	processes := remoteProcesses{uid: os.Getuid(), roots: []string{root}}
	otherUser := remoteProcesses{uid: os.Getuid() + 1, roots: []string{root}}
	if matches, err := otherUser.matches(server.Process.Pid); err != nil || matches {
		t.Fatalf("matched process for a different UID: matched=%v err=%v", matches, err)
	}
	if err := processes.stop(); err != nil {
		t.Fatal(err)
	}
	if alive, err := processes.matches(server.Process.Pid); err != nil || alive {
		t.Fatalf("server process still present: alive=%v err=%v", alive, err)
	}
	var exitErr *exec.ExitError
	if err := server.Wait(); !errors.As(err, &exitErr) || exitErr.Sys().(syscall.WaitStatus).Signal() != syscall.SIGTERM {
		t.Fatalf("expected server process to receive SIGTERM, got %v", err)
	}
	otherInstallation := remoteProcesses{uid: os.Getuid(), roots: []string{root + "-other"}}
	if alive, err := otherInstallation.matches(unrelated.Process.Pid); err != nil || !alive {
		t.Fatalf("unrelated process was stopped: alive=%v err=%v", alive, err)
	}
}

func TestServerProcessHelper(t *testing.T) {
	if os.Getenv("SERVERTOOL_TEST_VSCODE_SERVER") != "1" {
		return
	}
	if _, err := os.Stdout.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Minute)
}
