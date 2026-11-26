package manager_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/firewall/configuration/generate"
	"snail_tool/internal/firewall/internal/testenv"
	autostartconfig "snail_tool/internal/firewall/settings/autostart"
	"snail_tool/internal/storage"
	"snail_tool/internal/ui"
)

func TestEditMenuAutomaticallyValidatesAndLoadsSavedFile(t *testing.T) {
	env := testenv.New(t)
	env.CommandExists = func(name string) bool { return name == "nft" || name == "vim" }
	var operations []string
	env.CommandRun = func(name string, args ...string) error {
		if name != "vim" {
			t.Fatalf("unexpected editor: %s", name)
		}
		operations = append(operations, "edit")
		return os.WriteFile(env.EditPath(t, args), []byte("table inet snail {}\n"), 0600)
	}
	env.CommandOutput = func(name string, args ...string) (string, error) {
		if name == "nft" && reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
			return `{"nftables":[]}`, nil
		}
		if name != "nft" {
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		switch {
		case reflect.DeepEqual(args, []string{"-c", "-f", env.ConfigPath}):
			if storage.ReadFileString(env.ConfigPath) != "table inet snail {}\n" {
				t.Fatal("must validate the saved edit")
			}
			operations = append(operations, "validate")
		case reflect.DeepEqual(args, []string{"-f", env.ConfigPath}):
			operations = append(operations, "load")
		default:
			t.Fatalf("unexpected query: %s %v", name, args)
		}
		return "", nil
	}
	if err := env.Run(testenv.Input(t, "v\n\nq\n")); !errors.Is(err, ui.ErrReturnToMenu) {
		t.Fatal(err)
	}
	want := []string{"edit", "validate", "load"}
	if !reflect.DeepEqual(operations, want) {
		t.Fatalf("operations=%v want=%v", operations, want)
	}
	if storage.ReadFileString(env.ConfigPath) != "table inet snail {}\n" {
		t.Fatal("edited file must be retained after loading")
	}
}

func TestMenuReflectsAutostartChangeAndQueriesStateOncePerDisplay(t *testing.T) {
	env := testenv.New(t)
	env.CommandExists = func(string) bool { return true }
	content, _ := generate.Build([]int{2222}, false, false)
	if err := os.WriteFile(env.ConfigPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	state, commands, queries := "disabled", 0, 0
	env.CommandOutput = func(name string, args ...string) (string, error) {
		if name == "systemctl" {
			queries++
			return env.AutostartProperties(state), nil
		}
		if name == "nft" && reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
			return `{"nftables":[]}`, nil
		}
		if name == "nft" && reflect.DeepEqual(args, []string{"-c", "-f", env.ConfigPath}) {
			return "", nil
		}
		if name == "unshare" {
			return "", errors.New("network namespace unavailable")
		}
		t.Fatalf("unexpected command: %s %v", name, args)
		return "", nil
	}
	env.CommandRun = func(name string, args ...string) error {
		if name != "systemctl" || !reflect.DeepEqual(args, []string{"enable", autostartconfig.Unit}) {
			t.Fatalf("unexpected mutation: %s %v", name, args)
		}
		commands++
		state = "enabled"
		return nil
	}
	output := testenv.CaptureOutput(t, func() {
		if err := env.Run(testenv.Input(t, "8\n1\ny\n\nq\nq\n")); !errors.Is(err, ui.ErrReturnToMenu) {
			t.Fatal(err)
		}
	})
	if commands != 1 || queries != 7 {
		t.Fatalf("commands=%d queries=%d; expected four menu snapshots and three toggle checks", commands, queries)
	}
	if !strings.Contains(output, "开启开机自启") || !strings.Contains(output, "关闭开机自启") {
		t.Fatal("menu did not switch the displayed next action after enabling")
	}
}

func TestMenuEmptyInputRefreshesAndUppercaseReloadWorks(t *testing.T) {
	env := testenv.New(t)
	if err := os.WriteFile(env.ConfigPath, []byte("table inet snail {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var operations []string
	env.CommandOutput = func(name string, args ...string) (string, error) {
		if name != "nft" {
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		if reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
			return `{"nftables":[]}`, nil
		}
		operations = append(operations, strings.Join(args, " "))
		return "", nil
	}
	if err := env.Run(testenv.Input(t, "\nR\ny\n\nq\n")); !errors.Is(err, ui.ErrReturnToMenu) {
		t.Fatal(err)
	}
	want := []string{"-c -f " + env.ConfigPath, "-f " + env.ConfigPath}
	if !reflect.DeepEqual(operations, want) {
		t.Fatalf("empty input consumed an action or R failed: %v", operations)
	}
}

func TestOpenPortMenuShowsListenersWithoutChangingConfiguration(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	env := testenv.New(t)
	version := env.RuleConfig(t, nil)
	env.CommandExists = func(name string) bool { return name == "nft" }
	env.CommandOutput = func(name string, args ...string) (string, error) {
		switch {
		case name == "ss" && reflect.DeepEqual(args, []string{"-H", "-lntup"}):
			return "tcp LISTEN 0 511 127.0.0.1:80 0.0.0.0:* users:((\"nginx\",pid=1,fd=6))\n", nil
		case name == "nft" && reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}):
			return `{"nftables":[]}`, nil
		default:
			t.Fatalf("unexpected query: %s %v", name, args)
			return "", errors.New("unexpected query")
		}
	}
	output := testenv.CaptureOutput(t, func() {
		if err := env.Run(testenv.Input(t, "6\n\nq\n")); !errors.Is(err, ui.ErrReturnToMenu) {
			t.Fatal(err)
		}
	})
	if storage.ReadFileString(env.ConfigPath) != string(version.Content) {
		t.Fatal("open port view changed the firewall configuration")
	}
	for _, text := range []string{"查看开放端口", "127.0.0.1:80", `users:(("nginx",pid=1,fd=6))`, "└─", "不代表防火墙已经放行"} {
		if !strings.Contains(output, text) {
			t.Fatalf("missing %q in %s", text, output)
		}
	}
}

func TestTrafficMonitorMenuReturnsWithoutPauseOrConfigurationWrites(t *testing.T) {
	env := testenv.New(t)
	version := env.RuleConfig(t, nil)
	dir := t.TempDir()
	record := filepath.Join(dir, "nft-calls")
	t.Setenv("PATH", dir)
	t.Setenv("TEST_NFT_CALLS", record)
	const rules = "table inet snail { }"
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$TEST_NFT_CALLS\"\nprintf '%s\\n' '" + rules + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "nft"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	env.CommandOutput = func(name string, args ...string) (string, error) {
		if name != "nft" || !reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
			t.Fatalf("unexpected mutation or query: %s %v", name, args)
		}
		return `{"nftables":[]}`, nil
	}
	view := testenv.Input(t, "9\nq\nq\nnext\n")
	output := testenv.CaptureOutput(t, func() {
		if err := env.Run(view); !errors.Is(err, ui.ErrReturnToMenu) {
			t.Fatal(err)
		}
	})
	if storage.ReadFileString(record) != "-a list table inet snail\n" || storage.ReadFileString(env.ConfigPath) != string(version.Content) {
		t.Fatal("monitor queried after exit or changed configuration")
	}
	if _, err := os.Stat(env.DataDir); !os.IsNotExist(err) {
		t.Fatal("read-only monitor created software data or backup files")
	}
	if next, err := view.Ask("next"); err != nil || next != "next" {
		t.Fatal("return consumed another menu response")
	}
	for _, text := range []string{"实时查看规则", "nft -a list table inet snail", rules} {
		if !strings.Contains(output, text) {
			t.Fatalf("missing %q in %s", text, output)
		}
	}
}

const (
	ipv4Forwarding = "net.ipv4.ip_forward"
	ipv6Forwarding = "net.ipv6.conf.all.forwarding"
)

func writeForwardingValue(t *testing.T, env *testenv.Fixture, key, value string) {
	t.Helper()
	path := filepath.Join(env.ForwardingProcRoot, strings.ReplaceAll(key, ".", "/"))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestForwardingMenuAndBothCardsRefreshAfterChange(t *testing.T) {
	env := testenv.New(t)
	writeForwardingValue(t, env, "net.ipv4.ip_forward", "0")
	writeForwardingValue(t, env, "net.ipv6.conf.all.forwarding", "0")
	env.CommandExists = func(name string) bool { return name == "nft" || name == "sysctl" }
	env.CommandOutput = func(name string, args ...string) (string, error) {
		switch name {
		case "nft":
			if !reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
				t.Fatalf("unexpected nft command: %v", args)
			}
			return `{"nftables":[]}`, nil
		case "sysctl":
			if len(args) != 2 || args[0] != "-w" {
				t.Fatalf("unexpected sysctl command: %v", args)
			}
			key, value, _ := strings.Cut(args[1], "=")
			writeForwardingValue(t, env, key, value)
			return "", nil
		default:
			t.Fatalf("unexpected command: %s %v", name, args)
			return "", nil
		}
	}
	output := testenv.CaptureOutput(t, func() {
		if err := env.Run(testenv.Input(t, "8\n4\n1\ny\n\nq\nq\n")); !errors.Is(err, ui.ErrReturnToMenu) {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "IPv4 已关闭 · IPv6 已关闭") || strings.Count(output, "│ 内核转发：IPv4 已开启 · IPv6 已关闭") != 3 {
		t.Fatalf("both menus must refresh the kernel state after the success card: %s", output)
	}
	if !strings.Contains(output, "关闭 IPv4 转发    [当前]") || !strings.Contains(output, "[IPv4 已开启 · IPv6 已关闭]") {
		t.Fatal("forwarding options do not show the current state")
	}
}

func TestFirewallMenuOpensConfigAndPortPolicy(t *testing.T) {
	env := testenv.New(t)
	const config = "# firewall menu config\n"
	if err := os.WriteFile(env.ConfigPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	env.CommandOutput = func(name string, args ...string) (string, error) {
		if name != "nft" || !reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
			t.Fatalf("port view must not validate or apply configuration: %s %v", name, args)
		}
		return testenv.PortRules(), nil
	}
	output := testenv.CaptureOutput(t, func() {
		if err := env.Run(testenv.Input(t, "4\n\nc\n\nq\n")); !errors.Is(err, ui.ErrReturnToMenu) {
			t.Fatal(err)
		}
	})
	configStart := strings.Index(output, config)
	policyStart := strings.Index(output, "公网 IPv4 TCP")
	if configStart < 0 || policyStart <= configStart || !strings.Contains(output, "DHCPv6 客户端") {
		t.Fatal("expected 4 to display the configuration and c to display the loaded-port summary")
	}
}
