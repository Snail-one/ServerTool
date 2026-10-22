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

func TestPingSwitchPreservesOtherRulesAndPrecedesConnectionTracking(t *testing.T) {
	base, err := buildConfig([]int{2222}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate an older configuration without a dedicated Ping setting.
	legacy := shared.RemoveManagedBlock(base, pingBegin, pingEnd)
	legacy, err = withSSHPorts(legacy, []int{2222, 3022})
	if err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{true, false, true} {
		updated, err := withPingSetting(legacy, enabled)
		if err != nil {
			t.Fatal(err)
		}
		state, found, err := readPingSetting(updated)
		if err != nil || !found || state != enabled {
			t.Fatalf("incorrect setting: %v %v %v", state, found, err)
		}
		// Apart from the inserted block and whitespace, every existing rule stays.
		if !sameConfigTokens(shared.RemoveManagedBlock(updated, pingBegin, pingEnd), legacy) {
			t.Fatal("Ping switch changed SSH, ports, control traffic or manual rules")
		}
		if strings.Index(updated, pingBegin) > strings.Index(updated, "ct state established,related") || strings.Index(updated, pingBegin) > strings.Index(updated, sshPortsBegin) {
			t.Fatal("Ping must precede rules that could bypass its setting")
		}
		// Repeated SSH port changes must remain compatible with the Ping block.
		for i := 0; i < 2; i++ {
			updated, err = withSSHPorts(updated, []int{2222, 4022})
			if err != nil {
				t.Fatal(err)
			}
		}
		state, _, err = readPingSetting(updated)
		if err != nil || state != enabled || strings.Count(updated, pingBegin) != 1 || strings.Count(updated, sshPortsBegin) != 1 {
			t.Fatal("SSH updates damaged or duplicated Ping settings")
		}
	}
}

func TestPingRefusesDamagedUnreachableAndManuallyChangedBlocks(t *testing.T) {
	base, _ := buildConfig([]int{2222}, false, false)
	for _, content := range []string{
		strings.Replace(base, pingEnd, "", 1),
		base + "\n" + pingBegin,
		strings.Replace(base, pingBegin, "icmp type echo-request accept\n"+pingBegin, 1),
		strings.Replace(base, "counter drop comment \"snail:ping\"", "counter accept comment \"snail:ping\"", 1),
		strings.Replace(base, "hook input", "hook forward", 1),
		strings.Replace(base, "icmpv6 type echo-request", "icmpv6 type nd-neighbor-solicit", 1),
	} {
		if _, err := withPingSetting(content, true); err == nil {
			t.Fatal("unsafe or ambiguous Ping configuration accepted")
		}
	}
}

func TestConfigurePingValidationBackupLoadAndConcurrentEdits(t *testing.T) {
	for _, scenario := range []string{"on", "off", "invalid", "concurrent", "load failure"} {
		t.Run(scenario, func(t *testing.T) {
			setupTest(t)
			version := ruleTestConfig(t, orderTestRules())
			wantEnabled := scenario != "off"
			input := "1\ny\n"
			if !wantEnabled {
				on, _ := withPingSetting(string(version.content), true)
				if err := os.WriteFile(configPath, []byte(on), 0600); err != nil {
					t.Fatal(err)
				}
				version, _ = readConfigVersion()
				input = "2\ny\n"
			}
			var calls []string
			commandOutput = func(name string, args ...string) (string, error) {
				if name == "nft" && reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
					return `{"nftables":[]}`, nil
				}
				if name == "nft" && len(args) == 3 && args[0] == "-c" && args[1] == "-f" {
					calls = append(calls, "check")
					if scenario == "invalid" {
						return "syntax error", errors.New("invalid")
					}
					if scenario == "concurrent" {
						if err := os.WriteFile(configPath, []byte("# external edit\n"), 0600); err != nil {
							t.Fatal(err)
						}
					}
					return "", nil
				}
				if name == "nft" && reflect.DeepEqual(args, []string{"-f", configPath}) {
					calls = append(calls, "load")
					if scenario == "load failure" {
						return "load failed", errors.New("failed")
					}
					return "", nil
				}
				t.Fatalf("unexpected command: %s %v", name, args)
				return "", nil
			}
			err := configurePing(inputUI(t, input))
			backups, _ := filepath.Glob(filepath.Join(firewallBackupDir(), "nftables.conf.bak.*"))
			saved := shared.ReadFileString(configPath)
			if scenario == "invalid" || scenario == "concurrent" {
				want := string(version.content)
				if scenario == "concurrent" {
					want = "# external edit\n"
				}
				if err == nil || saved != want || len(backups) != 0 || !reflect.DeepEqual(calls, []string{"check"}) {
					t.Fatalf("unsafe write after failure: %v %v %v", err, calls, backups)
				}
				return
			}
			if (err != nil) != (scenario == "load failure") || !reflect.DeepEqual(calls, []string{"check", "load"}) {
				t.Fatalf("err=%v calls=%v", err, calls)
			}
			if scenario == "load failure" && !strings.Contains(err.Error(), "未成功加载") {
				t.Fatal("load failure must not claim success")
			}
			if len(backups) != 1 || shared.ReadFileString(backups[0]) != string(version.content) {
				t.Fatal("original configuration was not backed up exactly once")
			}
			state, found, err := readPingSetting(saved)
			if err != nil || !found || state != wantEnabled {
				t.Fatal("incorrect saved Ping setting")
			}
			got, err := managedRules(saved)
			if err != nil || !reflect.DeepEqual(got, orderTestRules()) {
				t.Fatal("Ping setting changed managed allow rules")
			}
		})
	}
}

func TestPingCancelDoesNotWriteOrConsumeNextInput(t *testing.T) {
	for _, input := range []string{"q\n", "返回\n", "1\nn\n", "2\n\n", "1\nq\n"} {
		t.Run(input, func(t *testing.T) {
			setupTest(t)
			version := ruleTestConfig(t, nil)
			commandOutput = func(name string, args ...string) (string, error) {
				if name == "nft" && reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
					return `{"nftables":[]}`, nil
				}
				t.Fatalf("unexpected query: %s %v", name, args)
				return "", nil
			}
			view := inputUI(t, input+"next\n")
			if err := configurePing(view); !errors.Is(err, shared.ErrReturnToMenu) {
				t.Fatalf("cancel did not return directly: %v", err)
			}
			if shared.ReadFileString(configPath) != string(version.content) {
				t.Fatal("cancel changed configuration")
			}
			if next, err := view.Ask("next"); err != nil || next != "next" {
				t.Fatal("cancel consumed extra input")
			}
		})
	}
}

func TestRegenerationKeepsPingEnabledAndMenuEntryWorks(t *testing.T) {
	setupTest(t)
	version := ruleTestConfig(t, nil)
	enabled, _ := withPingSetting(string(version.content), true)
	if err := os.WriteFile(configPath, []byte(enabled), 0600); err != nil {
		t.Fatal(err)
	}
	calls := acceptRuleCommands(t)
	accept := commandOutput
	commandOutput = func(name string, args ...string) (string, error) {
		if name == "ss" {
			return `LISTEN 0 128 *:2222 *:* users:(("sshd",pid=1,fd=3))`, nil
		}
		if name == "nft" && reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
			return `{"nftables":[]}`, nil
		}
		return accept(name, args...)
	}
	if err := configure(inputUI(t, "n\nn\n1\n")); err != nil {
		t.Fatal(err)
	}
	state, _, err := readPingSetting(shared.ReadFileString(configPath))
	if err != nil || !state {
		t.Fatal("regeneration lost enabled Ping")
	}
	if err := Run(inputUI(t, "8\n2\ny\n\nq\n")); !errors.Is(err, shared.ErrReturnToMenu) {
		t.Fatal(err)
	}
	state, _, err = readPingSetting(shared.ReadFileString(configPath))
	if err != nil || state || !reflect.DeepEqual(*calls, []string{"check", "load", "check", "load"}) {
		t.Fatalf("menu did not save and load disabled Ping: %v %v", err, *calls)
	}
}
