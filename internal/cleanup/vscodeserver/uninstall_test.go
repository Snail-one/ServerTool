package vscodeserver

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
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
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(sleep)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), ".vscode-server")
	start := func(path string) *exec.Cmd {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0700); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(path, "60")
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		})
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
