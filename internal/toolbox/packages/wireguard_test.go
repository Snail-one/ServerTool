package packages

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/shared"
)

func TestWireGuardMenuInstallsAndRemovesSystemPackage(t *testing.T) {
	setupWireGuardManagement(t)
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
		case name == "wg" && reflect.DeepEqual(args, []string{"show", "all", "listen-port"}):
			return "wg0\t51820\n", nil
		case name == "wg" && reflect.DeepEqual(args, []string{"show", "all", "peers"}):
			return "", nil
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
	err := RunWireGuard(newUIWithInput(t, "i\ny\n\nu\ny\n\nq\n"))
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

func TestWireGuardListsAndOpensInterfacesOnHomePage(t *testing.T) {
	setupWireGuardManagement(t)
	if err := os.MkdirAll(wireGuardConfigDir, 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 9; i++ {
		path := wireGuardProfilePath(fmt.Sprintf("wg%d", i))
		content := fmt.Sprintf("[Interface]\nAddress = 10.77.0.%d/24\nListenPort = 51820\n", i+1)
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	commandExists = func(name string) bool { return name == "wg" || name == "wg-quick" || name == "ip" }
	commandOutput = func(name string, args ...string) (string, error) {
		switch {
		case name == "wg" && reflect.DeepEqual(args, []string{"show", "interfaces"}):
			return "wg0 wgruntime\n", nil
		case name == "wg" && reflect.DeepEqual(args, []string{"--version"}):
			return "wireguard-tools v1.0\n", nil
		case name == "wg" && reflect.DeepEqual(args, []string{"show", "all", "listen-port"}):
			return "wg0\t56912\nwgruntime\t42899\n", nil
		case name == "wg" && reflect.DeepEqual(args, []string{"show", "all", "peers"}):
			return "wg0\tpeer1\nwg0\tpeer2\n", nil
		case name == "ip" && reflect.DeepEqual(args, []string{"-brief", "address", "show"}):
			return "wg0 UNKNOWN 10.77.0.20/24 fd77::20/64\nwgruntime UNKNOWN 10.88.0.1/24\n", nil
		default:
			t.Fatalf("unexpected query: %s %v", name, args)
			return "", nil
		}
	}
	view := newUIWithInput(t, "9\nq\nq\nnext\n")
	output := wireGuardTestOutput(t, func() {
		if err := RunWireGuard(view); !errors.Is(err, shared.ErrReturnToMenu) {
			t.Fatal(err)
		}
	})
	for _, want := range []string{
		"1   wg0", "9   wg8", "10  wgruntime", "WireGuard  ›  wg8", "已有配置", "仅运行时接口", "卸载 WireGuard 工具",
		"wg0：监听端口 UDP 56912 · 隧道地址 10.77.0.20/24, fd77::20/64 · 对端 2 个",
		"wg8：配置端口 UDP 51820 · 隧道地址 10.77.0.9/24 · 对端 0 个",
		"wgruntime：监听端口 UDP 42899 · 隧道地址 10.88.0.1/24 · 对端 0 个",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing menu output %q", want)
		}
	}
	if strings.Contains(output, "接口与配置") {
		t.Fatal("interface selection should not require an intermediate menu")
	}
	if next, err := view.ReadLine(); err != nil || next != "next" {
		t.Fatalf("unexpected remaining input: %q %v", next, err)
	}
}
