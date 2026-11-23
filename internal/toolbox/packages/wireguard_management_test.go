package packages

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

func setupWireGuardManagement(t *testing.T) string {
	t.Helper()
	restoreDependencies(t)
	configDir, dataDir, quick := wireGuardConfigDir, wireGuardDataDir, wireGuardQuick
	runtimeQuery := wireGuardRuntimeQuery
	t.Cleanup(func() {
		wireGuardConfigDir, wireGuardDataDir, wireGuardQuick = configDir, dataDir, quick
		wireGuardRuntimeQuery = runtimeQuery
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
		if name == "wg" {
			switch {
			case reflect.DeepEqual(args, []string{"genkey"}):
				return privateKey + "\n", nil
			case reflect.DeepEqual(args, []string{"genpsk"}):
				return preshared + "\n", nil
			}
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
		return os.WriteFile(path, []byte(strings.Replace(data, "Address = 10.77.0.1/24", "Address = 10.77.0.10/24", 1)), 0600)
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

	input := "1\ng\n" + peerKey + " 10.77.0.2/32 vpn.example.test:51820 25\ny\n\nq\nq\n"
	managerOutput := wireGuardTestOutput(t, func() {
		if err := runWireGuardInterface(newUIWithInput(t, input), "wg0"); !errors.Is(err, shared.ErrReturnToMenu) {
			t.Fatal(err)
		}
	})
	if !strings.Contains(managerOutput, "已保存对端：0 个") || !strings.Contains(managerOutput, "已保存对端：1 个") ||
		!strings.Contains(managerOutput, "已配置预共享密钥：1 个") {
		t.Fatal("peer overview should show the updated counts after adding a peer")
	}
	data, err := readWireGuardProfile("wg0")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("PresharedKey = "+preshared+"\n")) {
		t.Fatal("adding a peer must save its generated PSK")
	}
	output := wireGuardTestOutput(t, func() {
		if err := showWireGuardProfile("wg0"); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "WireGuard  ›  wg0  ›  查看配置") {
		t.Fatal("configuration details must have their own page title")
	}
	if !strings.Contains(output, peerKey) || !strings.Contains(output, "本机公钥") || !strings.Contains(output, "预共享密钥：已配置") ||
		strings.Contains(output, privateKey) || strings.Contains(output, preshared) {
		t.Fatal("profile display did not hide both secret keys or omitted public keys")
	}
	if err := manageWireGuardPeers(newUIWithInput(t, "1\n1\n2\n10.77.0.3/32\n3\n-\ns\ny\n\nq\n"), "wg0"); !errors.Is(err, shared.ErrReturnToMenu) {
		t.Fatal(err)
	}
	editedConfig, err := parseWireGuardConfig([]byte(shared.ReadFileString(wireGuardProfilePath("wg0"))))
	if err != nil || len(editedConfig.peers) != 1 {
		t.Fatalf("expected one edited peer: %v", err)
	}
	peer := editedConfig.peers[0]
	if wireGuardValue(peer, "allowedips") != "10.77.0.3/32" || wireGuardValue(peer, "endpoint") != "" ||
		wireGuardValue(peer, "presharedkey") != preshared || wireGuardValue(peer, "persistentkeepalive") != "25" {
		t.Fatal("editing the peer should change only the selected fields")
	}
	if err := manageWireGuardPeers(newUIWithInput(t, "1\n2\ny\n\nq\n"), "wg0"); !errors.Is(err, shared.ErrReturnToMenu) {
		t.Fatal(err)
	}
	if edited := shared.ReadFileString(wireGuardProfilePath("wg0")); strings.Contains(edited, "[Peer]") && strings.Contains(edited, peerKey) {
		t.Fatal("selected peer was not removed")
	}
	history, err := (shared.BackupStore{Root: wireGuardDataDir, Module: "wireguard"}).List()
	if err != nil || len(history) != 3 {
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
	wireGuardRuntimeQuery = func(_ context.Context, name string, args ...string) (string, error) {
		return commandOutput(name, args...)
	}
	commandExists = func(name string) bool { return name == "wg" || name == "wg-quick" || name == "systemctl" }
	commandOutput = func(name string, args ...string) (string, error) {
		switch name {
		case "wg":
			if reflect.DeepEqual(args, []string{"show", "wg0", "peers"}) {
				return "loaded-test-peer\n", nil
			}
			if reflect.DeepEqual(args, []string{"show", "wg0", "listen-port"}) {
				return "51820\n", nil
			}
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
	stoppedMenu := wireGuardTestOutput(t, func() {
		if err := runWireGuardInterface(newUIWithInput(t, "q\n"), "wg0"); !errors.Is(err, shared.ErrReturnToMenu) {
			t.Fatal(err)
		}
	})
	if !strings.Contains(stoppedMenu, "对端数量：已保存 0 个\n") {
		t.Fatal("a stopped interface should show the saved peer count")
	}
	offset := -1
	if strings.Contains(stoppedMenu, "3   添加对端") || strings.Contains(stoppedMenu, "g   快速配置") {
		t.Fatal("quick configuration belongs in the peer management menu")
	}
	for _, item := range []string{"1   管理对端", "2   查看配置", "3   编辑配置", "r   启动接口", "a   开启开机自启", "k   更换密钥", "x   删除配置"} {
		position := strings.Index(stoppedMenu, item)
		if position <= offset {
			t.Fatalf("menu item missing or out of order: %q", item)
		}
		offset = position
	}

	// The profile submenu should still be usable after each successful action.
	input := "r\ny\n\nC\n\nA\ny\n\nd\ny\n\nr\ny\n\na\ny\n\nq\n"
	var err error
	output := wireGuardTestOutput(t, func() {
		err = runWireGuardInterface(newUIWithInput(t, input), "wg0")
	})
	if !strings.Contains(output, "c   查看运行状态") || !strings.Contains(output, "监听端口：51820") {
		t.Fatal("running status should open from shortcut c")
	}
	if !strings.Contains(output, "对端数量：已保存 0 个 · 已加载 1 个") {
		t.Fatal("a running interface should show saved and loaded peer counts separately")
	}
	want := []string{"start", "enable", "restart", "stop", "disable"}
	if !errors.Is(err, shared.ErrReturnToMenu) || !reflect.DeepEqual(actions, want) || up || enabled {
		t.Fatalf("actions: %v, running: %v, autostart: %v, error: %v", actions, up, enabled, err)
	}
}

func TestWireGuardRuntimeRefreshesAndStopsOnReturn(t *testing.T) {
	setupWireGuardManagement(t)
	commandExists = func(name string) bool { return name == "wg" }
	blocked, cancelled := make(chan struct{}), make(chan struct{})
	localKey := "AbCd12" + strings.Repeat("x", 32) + "Yz345="
	peerKey := "EdFg67" + strings.Repeat("y", 32) + "Rt890="
	frames := 0
	wireGuardRuntimeQuery = func(ctx context.Context, _ string, args ...string) (string, error) {
		switch args[2] {
		case "public-key":
			frames++
			if frames == 3 {
				close(blocked)
				<-ctx.Done()
				close(cancelled)
				return "", ctx.Err()
			}
			return localKey + "\n", nil
		case "transfer":
			return fmt.Sprintf("%s\t%d\t180\n", peerKey, frames*92), nil
		}
		return "", nil
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	oldStdin := os.Stdin
	os.Stdin = reader
	view := ui.New()
	os.Stdin = oldStdin
	go func() {
		select {
		case <-blocked:
		case <-time.After(2 * time.Second):
		}
		_, _ = writer.WriteString("q\nnext\n")
	}()
	output := wireGuardTestOutput(t, func() {
		if err := monitorWireGuardRuntime(view, "wg0", time.Millisecond); !errors.Is(err, shared.ErrReturnToMenu) {
			t.Fatal(err)
		}
	})
	select {
	case <-cancelled:
	default:
		t.Fatal("return should cancel and wait for any status refresh")
	}
	if frames != 3 || !strings.Contains(output, "92 / 180") || !strings.Contains(output, "184 / 180") {
		t.Fatalf("expected two snapshots with different counters (queries=%d):\n%s", frames, output)
	}
	if !strings.Contains(output, "本机公钥：AbCd12…Yz345=") || !strings.Contains(output, "公钥：EdFg67…Rt890=") ||
		strings.Contains(output, localKey) || strings.Contains(output, peerKey) {
		t.Fatal("running status should show both public keys in abbreviated form")
	}
	if next, err := view.ReadLine(); err != nil || next != "next" {
		t.Fatalf("monitor should leave the next menu input intact: %q, %v", next, err)
	}
}

func TestWireGuardPeerKeepaliveRequiresExplicitInput(t *testing.T) {
	_, err := askWireGuardPeerField(newUIWithInput(t, "\n"), "PersistentKeepalive", "保活间隔（秒）")
	if !errors.Is(err, shared.ErrReturnToMenu) {
		t.Fatalf("Enter should leave the field as it was: %v", err)
	}
	for _, tc := range []struct{ input, want string }{{"0\n", "0"}, {"25\n", "25"}, {"-\n", ""}} {
		value, err := askWireGuardPeerField(newUIWithInput(t, tc.input), "PersistentKeepalive", "保活间隔（秒）")
		if err != nil || value != tc.want {
			t.Fatalf("explicit input should set %q, got %q: %v", tc.want, value, err)
		}
	}
}

func TestWireGuardQuickConfigRoundTrip(t *testing.T) {
	setupWireGuardManagement(t)
	key := func(value byte) string {
		return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{value}, 32))
	}
	if err := os.MkdirAll(wireGuardConfigDir, 0700); err != nil {
		t.Fatal(err)
	}
	remote := "[Interface]\nPrivateKey = " + key(2) + "\nAddress = 10.77.0.2/32\n"
	remoteConfig, err := parseWireGuardConfig([]byte(remote))
	if err != nil {
		t.Fatal(err)
	}
	local := "[Interface]\nPrivateKey = " + key(1) + "\nAddress = 10.77.0.1/24,fd77::1/64\n" +
		"ListenPort = 51820\n[Peer]\nPublicKey = " + wireGuardPublicKey(remoteConfig.device) +
		"\nAllowedIPs = 10.77.0.2/32\nPresharedKey = " + key(3) + "\n"
	for name, content := range map[string]string{"wg0": local, "wg1": remote} {
		if err := os.WriteFile(wireGuardProfilePath(name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	commandOutput = func(name string, args ...string) (string, error) {
		if name == "wg" && reflect.DeepEqual(args, []string{"show", "interfaces"}) {
			return "", nil
		}
		t.Fatalf("exporting or importing a supplied PSK should not generate a key: %s %v", name, args)
		return "", nil
	}
	output := wireGuardTestOutput(t, func() {
		if err := quickWireGuardConfig(newUIWithInput(t, "e\nvpn.example.test:51820\n1\nq\n"), "wg0"); !errors.Is(err, shared.ErrReturnToMenu) {
			t.Fatal(err)
		}
	})
	exported := ""
	for _, line := range strings.Split(output, "\n") {
		if strings.HasSuffix(line, " --psk="+key(3)) {
			exported = line
		}
	}
	if exported == "" || strings.Contains(output, key(1)) || strings.Count(output, key(3)) != 1 {
		t.Fatal("the copyable line should reveal only the explicitly selected PSK and no private key")
	}
	input := exported + "\ny\n"
	if err := quickWireGuardConfig(newUIWithInput(t, input), "wg1"); err != nil {
		t.Fatal(err)
	}
	config, err := parseWireGuardConfig([]byte(shared.ReadFileString(wireGuardProfilePath("wg1"))))
	if err != nil || len(config.peers) != 1 {
		t.Fatalf("missing imported peer: %v", err)
	}
	peer := config.peers[0]
	if wireGuardValue(peer, "presharedkey") != key(3) ||
		wireGuardValue(peer, "allowedips") != "10.77.0.1/32,fd77::1/128" ||
		wireGuardValue(peer, "endpoint") != "vpn.example.test:51820" {
		t.Fatal("import did not preserve the exported settings")
	}
}

func TestWireGuardRotateKeysFromInterfaceMenu(t *testing.T) {
	setupWireGuardManagement(t)
	key := func(value byte) string {
		return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{value}, 32))
	}
	if err := os.MkdirAll(wireGuardConfigDir, 0700); err != nil {
		t.Fatal(err)
	}
	path := wireGuardProfilePath("wg0")
	before := "[Interface]\nPrivateKey = " + key(1) + " # comment\nAddress = 10.77.0.1/24\n" +
		"\n[Peer]\nPublicKey = " + key(2) + "\nPresharedKey = " + key(3) + "\nAllowedIPs = 10.77.0.2/32\n" +
		"\n[Peer]\nPublicKey = " + key(4) + "\nPresharedKey = " + key(5) + "\nAllowedIPs = 10.77.0.4/32\n" +
		"\n[Peer]\nPublicKey = " + key(6) + "\nAllowedIPs = 10.77.0.6/32\n"
	if err := os.WriteFile(path, []byte(before), 0600); err != nil {
		t.Fatal(err)
	}
	commandExists = func(name string) bool { return name == "wg" }
	pskCalls := 0
	newPrivateKey := key(7)
	commandOutput = func(name string, args ...string) (string, error) {
		if name == "wg" {
			switch strings.Join(args, " ") {
			case "show interfaces":
				return "", nil
			case "genkey":
				return newPrivateKey + "\n", nil
			case "genpsk":
				pskCalls++
				return key(byte(7+pskCalls)) + "\n", nil
			}
		}
		t.Fatalf("unexpected command: %s %v", name, args)
		return "", nil
	}
	commandRun = func(name string, args ...string) error {
		t.Fatalf("key rotation must not start a stopped interface: %s %v", name, args)
		return nil
	}
	if err := rotateWireGuardKeys(newUIWithInput(t, "\n\n"), "wg0"); !errors.Is(err, shared.ErrReturnToMenu) {
		t.Fatal(err)
	}
	if after := shared.ReadFileString(path); after != before {
		t.Fatal("declining key rotation must keep the old configuration")
	}
	if err := rotateWireGuardKeys(newUIWithInput(t, "\ny\n"), "wg0"); err != nil {
		t.Fatal(err)
	}
	if after := shared.ReadFileString(path); after != strings.Replace(before, key(1), key(7), 1) || pskCalls != 0 {
		t.Fatal("default rotation must preserve preshared keys")
	}
	newPrivateKey = key(10)
	output := wireGuardTestOutput(t, func() {
		if err := runWireGuardInterface(newUIWithInput(t, "K\ny\ny\n\nq\n"), "wg0"); !errors.Is(err, shared.ErrReturnToMenu) {
			t.Fatal(err)
		}
	})
	want := strings.NewReplacer(key(1), key(10), key(3), key(8), key(5), key(9)).Replace(before)
	if after := shared.ReadFileString(path); after != want || pskCalls != 2 {
		t.Fatal("rotation must change just the private key and generate a separate key for each configured PSK")
	}
	for _, value := range []byte{1, 3, 5, 7, 8, 9, 10} {
		if strings.Contains(output, key(value)) {
			t.Fatal("rotation displayed a secret key")
		}
	}
}

func TestWireGuardRemovingPeerRestartsRunningInterface(t *testing.T) {
	for _, fails := range []bool{false, true} {
		t.Run(strconv.FormatBool(fails), func(t *testing.T) {
			setupWireGuardManagement(t)
			if err := os.MkdirAll(wireGuardConfigDir, 0700); err != nil {
				t.Fatal(err)
			}
			path := wireGuardProfilePath("wg0")
			base := "[Interface]\nAddress = 10.77.0.1/24\nSaveConfig = true\n"
			before := base + "\n[Peer]\nPublicKey = " + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)) +
				"\nAllowedIPs = 10.77.0.2/32\n"
			if err := os.WriteFile(path, []byte(before), 0600); err != nil {
				t.Fatal(err)
			}
			running := true
			commandExists = func(name string) bool { return name == "wg" || name == "wg-quick" }
			commandOutput = func(name string, args ...string) (string, error) {
				if name != "wg" || !reflect.DeepEqual(args, []string{"show", "interfaces"}) {
					t.Fatalf("unexpected command: %s %v", name, args)
				}
				if running {
					return "wg0\n", nil
				}
				return "", nil
			}
			var operations []string
			wireGuardQuick = func(args ...string) error {
				if len(args) != 2 || args[1] != path {
					t.Fatalf("unexpected wg-quick arguments: %v", args)
				}
				operations = append(operations, args[0])
				saved := shared.ReadFileString(path)
				switch args[0] {
				case "down":
					if saved != before {
						t.Fatal("old configuration must be in place when the interface stops")
					}
					running = false
					return os.WriteFile(path, []byte(before+"# written by SaveConfig\n"), 0600)
				case "up":
					if len(operations) == 2 && strings.Contains(saved, "[Peer]") {
						t.Fatal("removed peer must be absent when the interface starts")
					}
					if len(operations) == 2 && fails {
						return errors.New("simulated start failure")
					}
					running = true
					return nil
				default:
					t.Fatalf("unexpected action: %v", args)
					return nil
				}
			}
			config, err := parseWireGuardConfig([]byte(before))
			if err != nil {
				t.Fatal(err)
			}
			err = removeWireGuardPeer(newUIWithInput(t, "y\n"), "wg0", []byte(before), config, 0)
			if fails {
				if err == nil || !strings.Contains(err.Error(), "已恢复并启动旧配置") ||
					shared.ReadFileString(path) != before || !reflect.DeepEqual(operations, []string{"down", "up", "up"}) {
					t.Fatalf("failed restart did not restore the original configuration: %v, operations %v", err, operations)
				}
			} else if err != nil || strings.Contains(shared.ReadFileString(path), "[Peer]") ||
				!reflect.DeepEqual(operations, []string{"down", "up"}) {
				t.Fatalf("peer deletion was not saved and applied: %v, operations %v", err, operations)
			}
			if !running {
				t.Fatal("the interface must be running after a successful restart or rollback")
			}
		})
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
	if err := runWireGuardInterface(newUIWithInput(t, "x\ny\n\n"), "wg0"); !errors.Is(err, shared.ErrReturnToMenu) {
		t.Fatal(err)
	}
	if _, err := os.Stat(wireGuardProfilePath("wg0")); !os.IsNotExist(err) {
		t.Fatalf("deleted profile remains: %v", err)
	}
}

func TestWireGuardEnterUsesDefaults(t *testing.T) {
	setupWireGuardManagement(t)
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{4}, 32))
	var generated int
	commandOutput = func(name string, args ...string) (string, error) {
		if name != "wg" || !reflect.DeepEqual(args, []string{"genkey"}) {
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		generated++
		return key + "\n", nil
	}
	commandExists = func(name string) bool { return name == "vim" }
	commandRun = func(name string, args ...string) error {
		t.Fatalf("default setup must not launch an editor: %s %v", name, args)
		return nil
	}
	output := wireGuardTestOutput(t, func() {
		if err := createWireGuardProfile(newUIWithInput(t, "\n\n\ny\n")); err != nil {
			t.Fatal(err)
		}
	})
	content, err := readWireGuardProfile("wg0")
	if err != nil || generated != 1 {
		t.Fatalf("default profile not created as expected: key generations=%d, error=%v", generated, err)
	}
	config, err := parseWireGuardConfig(content)
	if err != nil || wireGuardValue(config.device, "privatekey") != key || wireGuardValue(config.device, "address") != "10.77.0.1/24" {
		t.Fatal("default profile has incorrect key or address")
	}
	listenPort := wireGuardValue(config.device, "listenport")
	port, err := strconv.Atoi(listenPort)
	if err != nil || port < 20000 || port > 60000 {
		t.Fatalf("invalid randomly generated listening port: %q", listenPort)
	}
	if !strings.Contains(output, "10.77.0.1/24") || !strings.Contains(output, listenPort) || strings.Contains(output, key) {
		t.Fatal("confirmation must display defaults and hide the private key")
	}
	if err := createWireGuardProfile(newUIWithInput(t, "\n")); err == nil || generated != 1 {
		t.Fatal("default setup must reject an existing wg0 profile")
	}
}

func TestWireGuardNewProfileWithFirstPeerPSK(t *testing.T) {
	for _, tc := range []struct {
		name, suffix string
		generatePSK  bool
	}{
		{"generated", "", true},
		{"skipped", " --no-psk", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupWireGuardManagement(t)
			privateKey := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
			peerKey := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
			psk := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32))
			commandOutput = func(name string, args ...string) (string, error) {
				switch {
				case name == "wg" && reflect.DeepEqual(args, []string{"genkey"}):
					return privateKey + "\n", nil
				case name == "wg" && reflect.DeepEqual(args, []string{"genpsk"}) && tc.generatePSK:
					return psk + "\n", nil
				default:
					t.Fatalf("unexpected command: %s %v", name, args)
					return "", nil
				}
			}
			output := wireGuardTestOutput(t, func() {
				input := "\n10.77.0.20/33\n10.77.0.20/32, fd77::20\n" + peerKey + " 10.77.0.2/32" + tc.suffix + "\ny\n"
				view := newUIWithInput(t, input)
				if err := createWireGuardProfile(view); err != nil {
					t.Fatal(err)
				}
			})
			after, err := readWireGuardProfile("wg0")
			if err != nil {
				t.Fatal(err)
			}
			config, err := parseWireGuardConfig(after)
			if err != nil || len(config.peers) != 1 {
				t.Fatalf("expected exactly one peer: %v", err)
			}
			if wireGuardValue(config.device, "address") != "10.77.0.20/32, fd77::20/128" {
				t.Fatal("manual addresses should be saved with their expected prefixes")
			}
			want := ""
			if tc.generatePSK {
				want = psk
			}
			if wireGuardValue(config.peers[0], "presharedkey") != want || strings.Contains(output, psk) || strings.Contains(output, privateKey) {
				t.Fatal("PSK was saved incorrectly or a secret was displayed")
			}
		})
	}
}

func TestWireGuardAddressAcceptsIPv4Suffix(t *testing.T) {
	for _, tc := range []struct {
		input, want string
	}{
		{"1\n", "10.77.0.1/24"},
		{"20\n", "10.77.0.20/24"},
		{"256\n255\n", "10.77.0.255/24"},
	} {
		address, err := askWireGuardAddress(newUIWithInput(t, tc.input))
		if err != nil || address != tc.want {
			t.Fatalf("want %q, got %q: %v", tc.want, address, err)
		}
	}
}

func TestWireGuardEditedTemplatePreservesProvidedPSK(t *testing.T) {
	setupWireGuardManagement(t)
	peer1 := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	peer2 := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32))
	existing := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{4}, 32))
	generated := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{5}, 32))
	calls := 0
	commandOutput = func(name string, args ...string) (string, error) {
		if name != "wg" || !reflect.DeepEqual(args, []string{"genpsk"}) {
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		calls++
		return generated + "\n", nil
	}
	before := "[Interface]\r\n[Peer]\r\nPublicKey = " + peer1 + "\r\nPresharedKey = " + existing +
		"\r\n[Peer]\r\nPublicKey = " + peer2 + "\r\n"
	after, err := withDefaultWireGuardPSKs([]byte(before))
	if err != nil {
		t.Fatal(err)
	}
	config, err := parseWireGuardConfig(after)
	if err != nil || len(config.peers) != 2 || calls != 1 {
		t.Fatalf("expected two peers and one generated key: calls=%d, err=%v", calls, err)
	}
	if wireGuardValue(config.peers[0], "presharedkey") != existing || wireGuardValue(config.peers[1], "presharedkey") != generated {
		t.Fatal("must preserve the supplied PSK and generate one for the other peer")
	}
}
