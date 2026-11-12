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

func TestForwardingChangesOnlySelectedProtocol(t *testing.T) {
	const v4 = "net.ipv4.ip_forward = 1"
	const v6 = "net.ipv6.conf.all.forwarding = 1"
	for _, scenario := range []struct {
		name, ipv4, ipv6, choice, saved, wantBody string
		wantCalls                                 []string
	}{
		{"enable IPv4", "0", "0", "1", "", v4, []string{"net.ipv4.ip_forward=1"}},
		{"enable IPv6", "0", "0", "3", "", v6, []string{"net.ipv6.conf.all.forwarding=1"}},
		{"enable IPv4 preserves IPv6", "0", "1", "1", v6, v4 + "\n" + v6, []string{"net.ipv4.ip_forward=1"}},
		{"enable IPv6 preserves IPv4", "1", "0", "3", v4, v4 + "\n" + v6, []string{"net.ipv6.conf.all.forwarding=1"}},
		{"disable IPv4 preserves IPv6", "1", "1", "2", v4 + "\n" + v6, v6, []string{"net.ipv4.ip_forward=0"}},
		{"disable IPv6 preserves IPv4", "1", "1", "4", v4 + "\n" + v6, v4, []string{"net.ipv6.conf.all.forwarding=0"}},
		{"disable last IPv4", "1", "1", "2", v4, "", []string{"net.ipv4.ip_forward=0"}},
		{"disable last IPv6", "1", "1", "4", v6, "", []string{"net.ipv6.conf.all.forwarding=0"}},
		{"disable missing config", "1", "1", "2", "", "", []string{"net.ipv4.ip_forward=0"}},
		{"already disabled", "0", "1", "2", v4, "", nil},
		{"already enabled", "1", "1", "1", "", v4, nil},
		{"IPv4 only", "0", "", "1", "", v4, []string{"net.ipv4.ip_forward=1"}},
		{"IPv6 only", "", "0", "3", "", v6, []string{"net.ipv6.conf.all.forwarding=1"}},
		{"other protocol unreadable", "invalid", "0", "3", "", v6, []string{"net.ipv6.conf.all.forwarding=1"}},
		{"retain unsupported protocol config", "1", "", "2", v4 + "\n" + v6, v6, []string{"net.ipv4.ip_forward=0"}},
		{"remove old disabled setting", "1", "0", "2", v4 + "\nnet.ipv6.conf.all.forwarding = 0", "", []string{"net.ipv4.ip_forward=0"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			setupTest(t)
			commandExists = func(name string) bool { return name == "sysctl" }
			if scenario.ipv4 != "" {
				writeForwardingValue(t, ipv4Forwarding, scenario.ipv4)
			}
			if scenario.ipv6 != "" {
				writeForwardingValue(t, ipv6Forwarding, scenario.ipv6)
			}
			if scenario.saved != "" {
				previous := shared.FormatManagedBlock(forwardingBegin, scenario.saved, forwardingEnd)
				if err := os.WriteFile(forwardingConfigPath, []byte(previous), 0644); err != nil {
					t.Fatal(err)
				}
			}
			checkConfig := func() {
				t.Helper()
				data, err := os.ReadFile(forwardingConfigPath)
				if scenario.wantBody == "" {
					if !os.IsNotExist(err) {
						t.Fatalf("config must be removed: %q err=%v", data, err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				body, found := shared.ManagedBlockContent(string(data), forwardingBegin, forwardingEnd)
				if !found || strings.TrimSpace(body) != scenario.wantBody {
					t.Fatalf("saved config=%q want=%q", body, scenario.wantBody)
				}
			}
			var calls []string
			commandOutput = func(name string, args ...string) (string, error) {
				if name != "sysctl" || len(args) != 2 || args[0] != "-w" {
					t.Fatalf("must only apply specific sysctl keys: %s %v", name, args)
				}
				checkConfig()
				calls = append(calls, args[1])
				key, value, _ := strings.Cut(args[1], "=")
				writeForwardingValue(t, key, value)
				return "", nil
			}
			if err := configureForwarding(inputUI(t, scenario.choice+"\ny\n")); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(calls, scenario.wantCalls) {
				t.Fatalf("calls=%v want=%v", calls, scenario.wantCalls)
			}
			checkConfig()
			other := inspectForwarding()[1]
			wantOther := scenario.ipv6
			if scenario.choice == "3" || scenario.choice == "4" {
				other, wantOther = inspectForwarding()[0], scenario.ipv4
			}
			if other.value != wantOther {
				t.Fatalf("unselected protocol changed: %+v want=%q", other, wantOther)
			}
			if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
				t.Fatal("forwarding settings must not create backup data")
			}
		})
	}
}

func TestForwardingDisableDeletesWholeFileWithoutBackup(t *testing.T) {
	setupTest(t)
	commandExists = func(name string) bool { return name == "sysctl" }
	writeForwardingValue(t, "net.ipv4.ip_forward", "0")
	previous := "# user setting\nnet.core.somaxconn = 1024\n\n" + shared.FormatManagedBlock(forwardingBegin, "net.ipv4.ip_forward = 1", forwardingEnd)
	if err := os.WriteFile(forwardingConfigPath, []byte(previous), 0600); err != nil {
		t.Fatal(err)
	}
	if err := configureForwarding(inputUI(t, "2\ny\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(forwardingConfigPath); !os.IsNotExist(err) {
		t.Fatalf("config must be deleted: %v", err)
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatal("disabling forwarding must not create backup data")
	}
}

func TestForwardingCancellationPreservesConfig(t *testing.T) {
	for _, input := range []string{"2\nn\n", "2\nq\n", "2\n", "5\nn\n", "5\nq\n", "5\n"} {
		t.Run(input, func(t *testing.T) {
			setupTest(t)
			commandExists = func(name string) bool { return name == "sysctl" }
			writeForwardingValue(t, "net.ipv4.ip_forward", "1")
			previous := shared.FormatManagedBlock(forwardingBegin, "net.ipv4.ip_forward = 1", forwardingEnd)
			if err := os.WriteFile(forwardingConfigPath, []byte(previous), 0644); err != nil {
				t.Fatal(err)
			}
			if err := configureForwarding(inputUI(t, input)); err == nil {
				t.Fatal("expected cancellation or EOF")
			}
			if shared.ReadFileString(forwardingConfigPath) != previous {
				t.Fatal("must preserve config on cancellation")
			}
			if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
				t.Fatal("must not create a backup on cancellation")
			}
		})
	}
}

func TestForwardingResetDeletesConfigAndDisablesBothProtocols(t *testing.T) {
	for _, scenario := range []struct {
		name, ipv4, ipv6, config string
		wantCalls                []string
	}{
		{"both enabled", "1", "1", "managed", []string{"net.ipv4.ip_forward=0", "net.ipv6.conf.all.forwarding=0"}},
		{"IPv4 enabled", "1", "0", "managed", []string{"net.ipv4.ip_forward=0"}},
		{"IPv6 enabled", "0", "1", "managed", []string{"net.ipv6.conf.all.forwarding=0"}},
		{"both disabled", "0", "0", "managed", nil},
		{"missing config", "1", "1", "missing", []string{"net.ipv4.ip_forward=0", "net.ipv6.conf.all.forwarding=0"}},
		{"IPv6 unsupported", "1", "", "managed", []string{"net.ipv4.ip_forward=0"}},
		{"damaged config", "1", "1", "damaged", []string{"net.ipv4.ip_forward=0", "net.ipv6.conf.all.forwarding=0"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			setupTest(t)
			commandExists = func(name string) bool { return name == "sysctl" }
			writeForwardingValue(t, ipv4Forwarding, scenario.ipv4)
			if scenario.ipv6 != "" {
				writeForwardingValue(t, ipv6Forwarding, scenario.ipv6)
			}
			if scenario.config != "missing" {
				previous := shared.FormatManagedBlock(forwardingBegin, "net.ipv4.ip_forward = 1\nnet.ipv6.conf.all.forwarding = 1", forwardingEnd)
				if scenario.config == "damaged" {
					previous = forwardingBegin + "\ninvalid content\n"
				}
				if err := os.WriteFile(forwardingConfigPath, []byte(previous), 0644); err != nil {
					t.Fatal(err)
				}
			}
			var calls []string
			commandOutput = func(name string, args ...string) (string, error) {
				if name != "sysctl" || len(args) != 2 || args[0] != "-w" {
					t.Fatalf("unexpected command: %s %v", name, args)
				}
				if _, err := os.Stat(forwardingConfigPath); !os.IsNotExist(err) {
					t.Fatal("must remove config before changing runtime state")
				}
				calls = append(calls, args[1])
				key, value, _ := strings.Cut(args[1], "=")
				writeForwardingValue(t, key, value)
				return "", nil
			}
			output := capturePortOutput(t, func() {
				if err := configureForwarding(inputUI(t, "5\ny\n")); err != nil {
					t.Fatal(err)
				}
			})
			if !strings.Contains(output, "恢复默认") || !strings.Contains(output, "内核转发已恢复默认") {
				t.Fatal("reset must be available in the menu and report success")
			}
			if !reflect.DeepEqual(calls, scenario.wantCalls) {
				t.Fatalf("calls=%v want=%v", calls, scenario.wantCalls)
			}
			for _, state := range inspectForwarding() {
				if !state.unavailable && (state.err != nil || state.value != "0") {
					t.Fatalf("reset must disable both supported protocols: %+v", state)
				}
			}
			if _, err := os.Stat(forwardingConfigPath); !os.IsNotExist(err) {
				t.Fatal("reset must remove the config file")
			}
			if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
				t.Fatal("reset must not create backup data")
			}
		})
	}
}

func TestForwardingResetReportsIncompleteRuntimeChanges(t *testing.T) {
	for _, scenario := range []string{"IPv4 failure", "IPv6 failure", "verification failure"} {
		t.Run(scenario, func(t *testing.T) {
			setupTest(t)
			commandExists = func(name string) bool { return name == "sysctl" }
			writeForwardingValue(t, ipv4Forwarding, "1")
			writeForwardingValue(t, ipv6Forwarding, "1")
			previous := shared.FormatManagedBlock(forwardingBegin, "net.ipv4.ip_forward = 1\nnet.ipv6.conf.all.forwarding = 1", forwardingEnd)
			if err := os.WriteFile(forwardingConfigPath, []byte(previous), 0644); err != nil {
				t.Fatal(err)
			}
			var calls []string
			commandOutput = func(name string, args ...string) (string, error) {
				if name != "sysctl" || len(args) != 2 || args[0] != "-w" {
					t.Fatalf("unexpected command: %s %v", name, args)
				}
				calls = append(calls, args[1])
				key, value, _ := strings.Cut(args[1], "=")
				if (scenario == "IPv4 failure" && key == ipv4Forwarding) || (scenario == "IPv6 failure" && key == ipv6Forwarding) {
					return "permission denied", errors.New("exit status 1")
				}
				if scenario != "verification failure" || key != ipv6Forwarding {
					writeForwardingValue(t, key, value)
				}
				return "", nil
			}
			err := configureForwarding(inputUI(t, "5\ny\n"))
			if err == nil || !strings.Contains(err.Error(), "持久配置已移除") || !strings.Contains(err.Error(), "未全部完成") {
				t.Fatalf("must report removed config and incomplete runtime changes: %v", err)
			}
			wantCalls := []string{"net.ipv4.ip_forward=0", "net.ipv6.conf.all.forwarding=0"}
			if !reflect.DeepEqual(calls, wantCalls) {
				t.Fatalf("must attempt both protocols even if the first fails: %v", calls)
			}
			if _, statErr := os.Stat(forwardingConfigPath); !os.IsNotExist(statErr) {
				t.Fatal("config must remain removed after runtime failure")
			}
		})
	}
}

func TestForwardingDisableReportsFailuresAfterRemovingConfig(t *testing.T) {
	for _, scenario := range []string{"command failure", "verification failure", "delete failure"} {
		t.Run(scenario, func(t *testing.T) {
			setupTest(t)
			if scenario == "delete failure" && os.Geteuid() == 0 {
				t.Skip("root bypasses directory permissions")
			}
			commandExists = func(name string) bool { return name == "sysctl" }
			writeForwardingValue(t, "net.ipv4.ip_forward", "1")
			writeForwardingValue(t, "net.ipv6.conf.all.forwarding", "1")
			previous := shared.FormatManagedBlock(forwardingBegin, "net.ipv4.ip_forward = 1", forwardingEnd)
			if err := os.WriteFile(forwardingConfigPath, []byte(previous), 0644); err != nil {
				t.Fatal(err)
			}
			if scenario == "delete failure" {
				parent := filepath.Dir(forwardingConfigPath)
				if err := os.Chmod(parent, 0500); err != nil {
					t.Fatal(err)
				}
				defer os.Chmod(parent, 0700)
			}
			calls := 0
			commandOutput = func(name string, args ...string) (string, error) {
				calls++
				if scenario == "command failure" {
					return "permission denied", errors.New("exit status 1")
				}
				return "", nil
			}
			err := configureForwarding(inputUI(t, "2\ny\n"))
			if err == nil {
				t.Fatal("must report failure")
			}
			if scenario == "delete failure" {
				if calls != 0 || shared.ReadFileString(forwardingConfigPath) != previous {
					t.Fatalf("must preserve runtime and config on failure: calls=%d err=%v", calls, err)
				}
			} else {
				if _, statErr := os.Stat(forwardingConfigPath); !os.IsNotExist(statErr) || !strings.Contains(err.Error(), "持久配置已移除") {
					t.Fatalf("must explain removed config but incomplete runtime change: %v", err)
				}
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

func TestForwardingRejectsInvalidSavedSettings(t *testing.T) {
	for _, body := range []string{
		"net.ipv4.ip_forward = invalid",
		"net.ipv6.conf.all.forwarding = 2",
		"net.ipv4.ip_forward = 1\nnet.ipv4.ip_forward = 0",
		"net.core.somaxconn = 1024",
	} {
		t.Run(body, func(t *testing.T) {
			setupTest(t)
			commandExists = func(name string) bool { return name == "sysctl" }
			writeForwardingValue(t, ipv4Forwarding, "0")
			previous := shared.FormatManagedBlock(forwardingBegin, body, forwardingEnd)
			if err := os.WriteFile(forwardingConfigPath, []byte(previous), 0644); err != nil {
				t.Fatal(err)
			}
			if err := configureForwarding(inputUI(t, "1\ny\n")); err == nil {
				t.Fatal("must reject invalid saved settings")
			}
			if shared.ReadFileString(forwardingConfigPath) != previous {
				t.Fatal("must preserve invalid config without writing")
			}
		})
	}
}

func TestForwardingCannotChangeUnavailableSelectedProtocol(t *testing.T) {
	for _, value := range []string{"", "invalid"} {
		t.Run(value, func(t *testing.T) {
			setupTest(t)
			commandExists = func(name string) bool { return name == "sysctl" }
			writeForwardingValue(t, ipv4Forwarding, "1")
			if value != "" {
				writeForwardingValue(t, ipv6Forwarding, value)
			}
			previous := shared.FormatManagedBlock(forwardingBegin, "net.ipv4.ip_forward = 1", forwardingEnd)
			if err := os.WriteFile(forwardingConfigPath, []byte(previous), 0644); err != nil {
				t.Fatal(err)
			}
			if err := configureForwarding(inputUI(t, "3\ny\n")); err == nil {
				t.Fatal("must reject unsupported or unreadable selected protocol")
			}
			if shared.ReadFileString(forwardingConfigPath) != previous {
				t.Fatal("must preserve the other protocol's config")
			}
		})
	}
}

func TestForwardingPreservesOtherSettingsWithoutBackup(t *testing.T) {
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
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatal("enabling forwarding must not create backup data")
	}
}

func TestForwardingReportsApplyAndVerificationFailures(t *testing.T) {
	for _, scenario := range []string{"command failure", "verification failure", "save failure"} {
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
				if scenario == "command failure" {
					return "permission denied", errors.New("exit status 1")
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
	if !strings.Contains(output, "IPv4 已关闭 · IPv6 已关闭") || strings.Count(output, "IPv4 已开启 · IPv6 已关闭") != 3 {
		t.Fatalf("both menus must refresh the kernel state after the success card: %s", output)
	}
}
