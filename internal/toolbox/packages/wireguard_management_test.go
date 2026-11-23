package packages

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/shared"
)

func setupWireGuardManagement(t *testing.T) string {
	t.Helper()
	restoreDependencies(t)
	configDir, dataDir, quick := wireGuardConfigDir, wireGuardDataDir, wireGuardQuick
	t.Cleanup(func() {
		wireGuardConfigDir, wireGuardDataDir, wireGuardQuick = configDir, dataDir, quick
	})
	root := t.TempDir()
	wireGuardConfigDir = filepath.Join(root, "etc", "wireguard")
	wireGuardDataDir = filepath.Join(root, "snailtool")
	isRoot = func() bool { return true }
	return root
}

func TestWireGuardConfigAndPeers(t *testing.T) {
	setupWireGuardManagement(t)
	privateKey := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	peerKey := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	preshared := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32))
	commandExists = func(name string) bool { return name == "vim" }
	commandOutput = func(name string, args ...string) (string, error) {
		if name == "wg" && reflect.DeepEqual(args, []string{"genkey"}) {
			return privateKey + "\n", nil
		}
		t.Fatalf("unexpected command: %s %v", name, args)
		return "", nil
	}
	commandRun = func(name string, args ...string) error {
		if name != "vim" || !reflect.DeepEqual(args[:3], []string{"-n", "-i", "NONE"}) {
			t.Fatalf("unexpected editor: %s %v", name, args)
		}
		path := args[len(args)-1]
		data := shared.ReadFileString(path)
		if !strings.Contains(data, privateKey) {
			t.Fatal("editing copy does not contain the generated key")
		}
		return os.WriteFile(path, []byte(strings.Replace(data, "# Address", "Address", 1)), 0600)
	}

	if err := createWireGuardProfile(newUIWithInput(t, "wg0\ny\n")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(wireGuardProfilePath("wg0"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatal("configuration permissions must be 0600")
	}

	if err := addWireGuardPeer(newUIWithInput(t, peerKey+" 10.77.0.2/32 vpn.example.test:51820 25\ny\n"), "wg0"); err != nil {
		t.Fatal(err)
	}
	data, err := readWireGuardProfile("wg0")
	if err != nil {
		t.Fatal(err)
	}
	content := append(data, []byte("PresharedKey = "+preshared+"\n")...)
	if err := os.WriteFile(wireGuardProfilePath("wg0"), content, 0600); err != nil {
		t.Fatal(err)
	}
	output := wireGuardTestOutput(t, func() {
		if err := showWireGuardProfile("wg0"); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, peerKey) || !strings.Contains(output, "本机公钥") ||
		strings.Contains(output, privateKey) || strings.Contains(output, preshared) {
		t.Fatal("profile display did not hide both secret keys or omitted public keys")
	}
	if err := removeWireGuardPeer(newUIWithInput(t, "1\ny\n"), "wg0"); err != nil {
		t.Fatal(err)
	}
	if edited := shared.ReadFileString(wireGuardProfilePath("wg0")); strings.Contains(edited, "[Peer]") && strings.Contains(edited, peerKey) {
		t.Fatal("selected peer was not removed")
	}
	history, err := (shared.BackupStore{Root: wireGuardDataDir, Module: "wireguard"}).List()
	if err != nil || len(history) != 2 {
		t.Fatalf("expected a backup for each peer change: %v %v", history, err)
	}
}

func TestWireGuardRuntimeAndBootControls(t *testing.T) {
	setupWireGuardManagement(t)
	if err := os.MkdirAll(wireGuardConfigDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wireGuardProfilePath("wg0"), []byte("[Interface]\nAddress = 10.77.0.1/24\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var up, enabled bool
	commandExists = func(name string) bool { return name == "wg" || name == "wg-quick" || name == "systemctl" }
	commandOutput = func(name string, args ...string) (string, error) {
		switch name {
		case "wg":
			if up {
				return "wg0\n", nil
			}
			return "", nil
		case "systemctl":
			activeState, bootState := "inactive", "disabled"
			if up {
				activeState = "active"
			}
			if enabled {
				bootState = "enabled"
			}
			return "LoadState=loaded\nActiveState=" + activeState + "\nUnitFileState=" + bootState + "\n", nil
		default:
			t.Fatalf("unexpected command: %s %v", name, args)
			return "", nil
		}
	}
	var actions []string
	commandRun = func(name string, args ...string) error {
		if name != "systemctl" || len(args) != 2 || args[1] != "wg-quick@wg0.service" {
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		actions = append(actions, args[0])
		switch args[0] {
		case "start", "restart":
			up = true
		case "stop":
			up = false
		case "enable":
			enabled = true
		case "disable":
			enabled = false
		}
		return nil
	}
	// The profile submenu should still be usable after each successful action.
	input := "4\ny\n\n6\ny\n\n5\ny\n\n4\ny\n\n6\ny\n\nq\n"
	err := runWireGuardInterface(newUIWithInput(t, input), "wg0")
	want := []string{"start", "enable", "restart", "stop", "disable"}
	if !errors.Is(err, shared.ErrReturnToMenu) || !reflect.DeepEqual(actions, want) || up || enabled {
		t.Fatalf("actions: %v, running: %v, autostart: %v, error: %v", actions, up, enabled, err)
	}
}

func wireGuardTestOutput(t *testing.T, run func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	old := os.Stdout
	os.Stdout = writer
	defer func() { os.Stdout = old }()
	run()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(output)
}

func TestWireGuardUsesWGQuickWithoutSystemd(t *testing.T) {
	setupWireGuardManagement(t)
	if err := os.MkdirAll(wireGuardConfigDir, 0700); err != nil {
		t.Fatal(err)
	}
	data := []byte("[Interface]\nAddress = 10.77.0.1/24\n")
	if err := os.WriteFile(wireGuardProfilePath("wg0"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := createWireGuardProfile(newUIWithInput(t, "wg0\n")); err == nil {
		t.Fatal("creation should refuse to replace an existing profile")
	}
	if err := createWireGuardProfile(newUIWithInput(t, "../wg0\n")); err == nil {
		t.Fatal("invalid name must be rejected")
	}

	commandExists = func(name string) bool { return name == "wg" || name == "wg-quick" }
	active := false
	commandOutput = func(name string, args ...string) (string, error) {
		if name != "wg" || !reflect.DeepEqual(args, []string{"show", "interfaces"}) {
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		if active {
			return "wg0\n", nil
		}
		return "", nil
	}
	var operations []string
	wireGuardQuick = func(args ...string) error {
		if len(args) != 2 || args[1] != wireGuardProfilePath("wg0") {
			t.Fatalf("unexpected wg-quick arguments: %v", args)
		}
		operations = append(operations, args[0])
		active = args[0] == "up"
		return nil
	}
	for _, action := range []string{"start", "restart", "stop"} {
		if err := changeWireGuardRuntime(newUIWithInput(t, "y\n"), "wg0", action); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(operations, []string{"up", "down", "up", "down"}) || active {
		t.Fatalf("unexpected operations or runtime state: %v, active: %t", operations, active)
	}
	if err := deleteWireGuardProfile(newUIWithInput(t, "y\n"), "wg0"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wireGuardProfilePath("wg0")); !os.IsNotExist(err) {
		t.Fatalf("deleted profile remains: %v", err)
	}
}
