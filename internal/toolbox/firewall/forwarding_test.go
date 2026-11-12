package firewall

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/shared"
)

func writeForwardingValue(t *testing.T, key, value string) {
	t.Helper()
	path := filepath.Join(forwardingProcRoot, strings.ReplaceAll(key, ".", "/"))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestForwardingStatusReadsKernelAndDistinguishesErrors(t *testing.T) {
	for _, scenario := range []struct {
		ipv4, ipv6, want4, want6 string
		invalid                  bool
	}{
		{"1", "0", "已开启", "已关闭", false},
		{"0", "1", "已关闭", "已开启", false},
		{"1", "", "已开启", "不支持", false},
		{"invalid", "0", "检测失败", "已关闭", true},
		{"", "", "不支持", "不支持", true},
	} {
		t.Run(scenario.ipv4+"/"+scenario.ipv6, func(t *testing.T) {
			setupTest(t)
			if scenario.ipv4 != "" {
				writeForwardingValue(t, "net.ipv4.ip_forward", scenario.ipv4)
			}
			if scenario.ipv6 != "" {
				writeForwardingValue(t, "net.ipv6.conf.all.forwarding", scenario.ipv6)
			}
			states := inspectForwarding()
			field := forwardingStateField(states)
			if field.Label != "内核转发" || !strings.Contains(field.Value, "IPv4 "+scenario.want4) || !strings.Contains(field.Value, "IPv6 "+scenario.want6) {
				t.Fatalf("unexpected status: %+v", field)
			}
			if err := validateForwarding(states); (err != nil) != scenario.invalid {
				t.Fatalf("invalid=%v err=%v", scenario.invalid, err)
			}
		})
	}
}

func TestForwardingAppliesAndPersistsSupportedProtocols(t *testing.T) {
	for _, scenario := range []struct {
		name, ipv4, ipv6, choice, target string
		want                             []string
	}{
		{"enable", "0", "0", "1", "1", []string{"net.ipv4.ip_forward=1", "net.ipv6.conf.all.forwarding=1"}},
		{"disable", "1", "1", "2", "0", []string{"net.ipv4.ip_forward=0", "net.ipv6.conf.all.forwarding=0"}},
		{"mixed", "1", "0", "1", "1", []string{"net.ipv6.conf.all.forwarding=1"}},
		{"IPv4 only", "0", "", "1", "1", []string{"net.ipv4.ip_forward=1"}},
		{"already enabled", "1", "1", "1", "1", nil},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			setupTest(t)
			commandExists = func(name string) bool { return name == "sysctl" }
			writeForwardingValue(t, "net.ipv4.ip_forward", scenario.ipv4)
			if scenario.ipv6 != "" {
				writeForwardingValue(t, "net.ipv6.conf.all.forwarding", scenario.ipv6)
			}
			var calls []string
			commandOutput = func(name string, args ...string) (string, error) {
				if name != "sysctl" || len(args) != 2 || args[0] != "-w" {
					t.Fatalf("must only apply specific sysctl keys: %s %v", name, args)
				}
				calls = append(calls, args[1])
				key, value, _ := strings.Cut(args[1], "=")
				writeForwardingValue(t, key, value)
				return "", nil
			}
			if err := configureForwarding(inputUI(t, scenario.choice+"\ny\n")); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(calls, scenario.want) {
				t.Fatalf("calls=%v want=%v", calls, scenario.want)
			}
			data, err := os.ReadFile(forwardingConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), "net.ipv4.ip_forward = "+scenario.target) {
				t.Fatalf("missing persistent IPv4 setting: %s", data)
			}
			if scenario.ipv6 == "" {
				if strings.Contains(string(data), "net.ipv6") {
					t.Fatal("must not persist unsupported IPv6 settings")
				}
			} else if !strings.Contains(string(data), "net.ipv6.conf.all.forwarding = "+scenario.target) {
				t.Fatalf("missing persistent IPv6 setting: %s", data)
			}
		})
	}
}

func TestForwardingRefusesUnconfirmedOrInvalidChanges(t *testing.T) {
	for _, scenario := range []string{"cancel", "back", "EOF", "nonroot", "missing sysctl", "invalid state", "unsupported", "broken markers", "symlink"} {
		t.Run(scenario, func(t *testing.T) {
			setupTest(t)
			commandExists = func(name string) bool { return name == "sysctl" && scenario != "missing sysctl" }
			isRoot = func() bool { return scenario != "nonroot" }
			if scenario != "unsupported" {
				value := "0"
				if scenario == "invalid state" {
					value = "unexpected"
				}
				writeForwardingValue(t, "net.ipv4.ip_forward", value)
			}
			previous := "# keep this file\n"
			if scenario == "broken markers" {
				previous += forwardingBegin + "\n"
			}
			if err := os.WriteFile(forwardingConfigPath, []byte(previous), 0644); err != nil {
				t.Fatal(err)
			}
			if scenario == "symlink" {
				target := forwardingConfigPath
				forwardingConfigPath += ".link"
				if err := os.Symlink(target, forwardingConfigPath); err != nil {
					t.Fatal(err)
				}
			}
			input := "1\ny\n"
			switch scenario {
			case "cancel":
				input = "1\nn\n"
			case "back":
				input = "q\n"
			case "EOF":
				input = "1\n"
			}
			if err := configureForwarding(inputUI(t, input)); err == nil {
				t.Fatal("expected cancellation or error")
			}
			data, err := os.ReadFile(forwardingConfigPath)
			if err != nil || string(data) != previous {
				t.Fatalf("file changed: %q err=%v", data, err)
			}
			if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
				t.Fatal("must not back up or write before confirmed valid changes")
			}
		})
	}
}

func TestForwardingPreservesOtherSettingsAndBacksUpPreviousFile(t *testing.T) {
	setupTest(t)
	commandExists = func(name string) bool { return name == "sysctl" }
	writeForwardingValue(t, "net.ipv4.ip_forward", "1")
	previous := "# user setting\nnet.core.somaxconn = 1024\n\n" + shared.FormatManagedBlock(forwardingBegin, "net.ipv4.ip_forward = 0", forwardingEnd)
	if err := os.WriteFile(forwardingConfigPath, []byte(previous), 0600); err != nil {
		t.Fatal(err)
	}
	if err := configureForwarding(inputUI(t, "1\ny\n")); err != nil {
		t.Fatal(err)
	}
	updated := shared.ReadFileString(forwardingConfigPath)
	if !strings.Contains(updated, "net.core.somaxconn = 1024") || strings.Count(updated, forwardingBegin) != 1 || !strings.Contains(updated, "net.ipv4.ip_forward = 1") {
		t.Fatalf("unexpected config: %s", updated)
	}
	info, _ := os.Stat(forwardingConfigPath)
	if info.Mode().Perm() != 0600 {
		t.Fatal("must preserve existing file permissions")
	}
	store := shared.BackupStore{Root: dataDir, Module: "firewall"}
	dir, err := store.Directory(forwardingConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	backups, err := filepath.Glob(filepath.Join(dir, filepath.Base(forwardingConfigPath)+".bak.*"))
	if err != nil || len(backups) != 1 || shared.ReadFileString(backups[0]) != previous {
		t.Fatalf("previous config not backed up: %v err=%v", backups, err)
	}
}

func TestForwardingReportsApplyAndVerificationFailures(t *testing.T) {
	for _, scenario := range []string{"command failure", "partial failure", "verification failure", "save failure"} {
		t.Run(scenario, func(t *testing.T) {
			setupTest(t)
			commandExists = func(name string) bool { return name == "sysctl" }
			writeForwardingValue(t, "net.ipv4.ip_forward", "0")
			writeForwardingValue(t, "net.ipv6.conf.all.forwarding", "0")
			if scenario == "save failure" {
				forwardingConfigPath = filepath.Join(t.TempDir(), "missing-parent", "forwarding.conf")
			}
			calls := 0
			commandOutput = func(name string, args ...string) (string, error) {
				calls++
				if scenario == "command failure" || (scenario == "partial failure" && calls == 2) {
					return "permission denied", errors.New("exit status 1")
				}
				if scenario == "partial failure" {
					key, value, _ := strings.Cut(args[1], "=")
					writeForwardingValue(t, key, value)
				}
				return "", nil
			}
			err := configureForwarding(inputUI(t, "1\ny\n"))
			if err == nil {
				t.Fatal("must report failure")
			}
			if scenario == "save failure" {
				if calls != 0 || !strings.Contains(err.Error(), "尚未修改运行状态") {
					t.Fatalf("must not apply after save failure: calls=%d err=%v", calls, err)
				}
			} else if !strings.Contains(err.Error(), "配置已保存") || !strings.Contains(shared.ReadFileString(forwardingConfigPath), "net.ipv4.ip_forward = 1") {
				t.Fatalf("must explain saved but incompletely applied configuration: %v", err)
			}
		})
	}
}

func TestForwardingMenuAndBothCardsRefreshAfterChange(t *testing.T) {
	setupTest(t)
	writeForwardingValue(t, "net.ipv4.ip_forward", "0")
	writeForwardingValue(t, "net.ipv6.conf.all.forwarding", "0")
	commandExists = func(name string) bool { return name == "nft" || name == "sysctl" }
	commandOutput = func(name string, args ...string) (string, error) {
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
			writeForwardingValue(t, key, value)
			return "", nil
		default:
			t.Fatalf("unexpected command: %s %v", name, args)
			return "", nil
		}
	}
	output := capturePortOutput(t, func() {
		if err := Run(inputUI(t, "7\n4\n1\ny\n\nq\nq\n")); !errors.Is(err, shared.ErrReturnToMenu) {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "IPv4 已关闭 · IPv6 已关闭") || strings.Count(output, "IPv4 已开启 · IPv6 已开启") != 3 {
		t.Fatalf("both menus must refresh the kernel state after the success card: %s", output)
	}
}
