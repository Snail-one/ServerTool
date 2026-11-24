package firewall

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/firewall/configuration/generate"
	autostartconfig "snail_tool/internal/firewall/settings/autostart"
	"snail_tool/internal/storage"
	"snail_tool/internal/ui"
)

func TestEditMenuAutomaticallyValidatesAndLoadsSavedFile(t *testing.T) {
	setupTest(t)
	commandExists = func(name string) bool { return name == "nft" || name == "vim" }
	var operations []string
	commandRun = func(name string, args ...string) error {
		if name != "vim" {
			t.Fatalf("unexpected editor: %s", name)
		}
		operations = append(operations, "edit")
		return os.WriteFile(firewallEditPath(t, args), []byte("table inet snail {}\n"), 0600)
	}
	commandOutput = func(name string, args ...string) (string, error) {
		if name == "nft" && reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
			return `{"nftables":[]}`, nil
		}
		if name != "nft" {
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		switch {
		case reflect.DeepEqual(args, []string{"-c", "-f", configPath}):
			if storage.ReadFileString(configPath) != "table inet snail {}\n" {
				t.Fatal("must validate the saved edit")
			}
			operations = append(operations, "validate")
		case reflect.DeepEqual(args, []string{"-f", configPath}):
			operations = append(operations, "load")
		default:
			t.Fatalf("unexpected query: %s %v", name, args)
		}
		return "", nil
	}
	if err := Run(inputUI(t, "v\n\nq\n")); !errors.Is(err, ui.ErrReturnToMenu) {
		t.Fatal(err)
	}
	want := []string{"edit", "validate", "load"}
	if !reflect.DeepEqual(operations, want) {
		t.Fatalf("operations=%v want=%v", operations, want)
	}
	if storage.ReadFileString(configPath) != "table inet snail {}\n" {
		t.Fatal("edited file must be retained after loading")
	}
}

func TestMenuReflectsAutostartChangeAndQueriesStateOncePerDisplay(t *testing.T) {
	setupTest(t)
	commandExists = func(string) bool { return true }
	content, _ := generate.Build([]int{2222}, false, false)
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	state, commands, queries := "disabled", 0, 0
	commandOutput = func(name string, args ...string) (string, error) {
		if name == "systemctl" {
			queries++
			return autostartProperties(state), nil
		}
		if name == "nft" && reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
			return `{"nftables":[]}`, nil
		}
		if name == "nft" && reflect.DeepEqual(args, []string{"-c", "-f", configPath}) {
			return "", nil
		}
		if name == "unshare" {
			return "", errors.New("network namespace unavailable")
		}
		t.Fatalf("unexpected command: %s %v", name, args)
		return "", nil
	}
	commandRun = func(name string, args ...string) error {
		if name != "systemctl" || !reflect.DeepEqual(args, []string{"enable", autostartconfig.Unit}) {
			t.Fatalf("unexpected mutation: %s %v", name, args)
		}
		commands++
		state = "enabled"
		return nil
	}
	output := capturePortOutput(t, func() {
		if err := Run(inputUI(t, "8\n1\ny\n\nq\nq\n")); !errors.Is(err, ui.ErrReturnToMenu) {
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
	setupTest(t)
	if err := os.WriteFile(configPath, []byte("table inet snail {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var operations []string
	commandOutput = func(name string, args ...string) (string, error) {
		if name != "nft" {
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		if reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
			return `{"nftables":[]}`, nil
		}
		operations = append(operations, strings.Join(args, " "))
		return "", nil
	}
	if err := Run(inputUI(t, "\nR\ny\n\nq\n")); !errors.Is(err, ui.ErrReturnToMenu) {
		t.Fatal(err)
	}
	want := []string{"-c -f " + configPath, "-f " + configPath}
	if !reflect.DeepEqual(operations, want) {
		t.Fatalf("empty input consumed an action or R failed: %v", operations)
	}
}

func TestTrafficMonitorMenuReturnsWithoutPauseOrConfigurationWrites(t *testing.T) {
	setupTest(t)
	version := ruleTestConfig(t, nil)
	dir := t.TempDir()
	record := filepath.Join(dir, "nft-calls")
	t.Setenv("PATH", dir)
	t.Setenv("TEST_NFT_CALLS", record)
	const rules = "table inet snail { }"
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$TEST_NFT_CALLS\"\nprintf '%s\\n' '" + rules + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "nft"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	commandOutput = func(name string, args ...string) (string, error) {
		if name != "nft" || !reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
			t.Fatalf("unexpected mutation or query: %s %v", name, args)
		}
		return `{"nftables":[]}`, nil
	}
	view := inputUI(t, "9\nq\nq\nnext\n")
	output := capturePortOutput(t, func() {
		if err := Run(view); !errors.Is(err, ui.ErrReturnToMenu) {
			t.Fatal(err)
		}
	})
	if storage.ReadFileString(record) != "-a list table inet snail\n" || storage.ReadFileString(configPath) != string(version.Content) {
		t.Fatal("monitor queried after exit or changed configuration")
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
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
