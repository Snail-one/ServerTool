package packages

import (
	"errors"
	"reflect"
	"testing"

	"snail_tool/internal/shared"
)

func TestWireGuardMenuInstallsAndRemovesSystemPackage(t *testing.T) {
	restoreDependencies(t)
	installed := false
	isRoot = func() bool { return true }
	commandExists = func(name string) bool {
		return name == "apt-get" || installed && (name == "wg" || name == "wg-quick")
	}
	commandOutput = func(name string, args ...string) (string, error) {
		switch {
		case name == "dpkg-query":
			if installed {
				return "wireguard-tools\tinstall ok installed\n", nil
			}
			return "", nil
		case name == "wg" && reflect.DeepEqual(args, []string{"--version"}):
			return "wireguard-tools v1.0.20250521\n", nil
		case name == "wg" && reflect.DeepEqual(args, []string{"show", "interfaces"}):
			return "wg0\n", nil
		default:
			t.Fatalf("unexpected query: %s %v", name, args)
			return "", nil
		}
	}
	var calls [][]string
	commandRun = func(name string, args ...string) error {
		calls = append(calls, append([]string{name}, args...))
		switch args[0] {
		case "install":
			installed = true
		case "remove":
			installed = false
		}
		return nil
	}
	err := RunWireGuard(newUIWithInput(t, "8\ny\n\n9\ny\n\nq\n"))
	if !errors.Is(err, shared.ErrReturnToMenu) || installed {
		t.Fatalf("uninstall or navigation did not finish: installed=%v error=%v", installed, err)
	}
	want := [][]string{
		{"apt-get", "update"},
		{"apt-get", "install", "-y", "wireguard-tools"},
		{"apt-get", "remove", "-y", "wireguard-tools"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("package operations: %v; want %v", calls, want)
	}
}

func TestWireGuardRejectsUnmanagedToolsBeforeRemoval(t *testing.T) {
	restoreDependencies(t)
	isRoot = func() bool { return true }
	commandExists = func(name string) bool {
		return name == "apt-get" || name == "wg" || name == "wg-quick"
	}
	commandOutput = func(string, ...string) (string, error) { return "", nil }
	commandRun = func(name string, args ...string) error {
		t.Fatalf("unmanaged tools must not be removed: %s %v", name, args)
		return nil
	}
	if err := uninstallWireGuard(newUIWithInput(t, "y\n")); err == nil {
		t.Fatal("expected an error for tools unmanaged by the package manager")
	}
}
