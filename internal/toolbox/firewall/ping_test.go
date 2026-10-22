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
		if strings.Index(updated, pingBegin) > strings.Index(updated, "ct state established,related") {
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
		if err != nil || state != enabled || strings.Count(updated, pingBegin) != 1 ||
			!strings.Contains(updated, "define snail_ssh_port_2 = 4022") || strings.Contains(updated, sshPortsBegin) {
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
		if _, err := withoutPingSetting(content); err == nil {
			t.Fatal("damaged Ping configuration must not be deleted")
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
	for _, input := range []string{"q\n", "返回\n", "1\nn\n", "2\n\n", "1\nq\n", "3\nn\n", "3\nq\n"} {
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

func TestRemovePingConfigurationKeepsOtherRulesAndDoesNotRecreateOnRegeneration(t *testing.T) {
	for _, scenario := range []string{"allowed", "blocked", "not set"} {
		t.Run(scenario, func(t *testing.T) {
			setupTest(t)
			version := ruleTestConfig(t, orderTestRules())
			content, err := withPingSetting(string(version.content), scenario == "allowed")
			if scenario == "not set" {
				content, err = withoutPingSetting(content)
			}
			if err != nil {
				t.Fatal(err)
			}
			content, err = withSSHPorts(content, []int{2222, 3022})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			calls := acceptRuleCommands(t)
			accept := commandOutput
			commandOutput = func(name string, args ...string) (string, error) {
				if name == "nft" && reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
					return `{"nftables":[]}`, nil
				}
				if isSSHDTest(name, args) {
					return sshdTestConfig(2222), nil
				}
				return accept(name, args...)
			}
			input := "3\ny\nnext\n"
			if scenario == "not set" {
				input = "3\nnext\n"
			}
			view := inputUI(t, input)
			if err := configurePing(view); err != nil {
				t.Fatal(err)
			}
			if next, err := view.Ask("next"); err != nil || next != "next" {
				t.Fatal("removal consumed an unexpected confirmation or pause")
			}
			saved := shared.ReadFileString(configPath)
			if strings.Contains(saved, "snail:ping") || strings.Contains(saved, pingBegin) || strings.Contains(saved, pingEnd) {
				t.Fatal("dedicated Ping rules or markers remain")
			}
			// Every other active directive, including SSH, is preserved.
			without, _ := withoutPingSetting(content)
			if !sameConfigTokens(saved, without) || !strings.Contains(saved, "define snail_ssh_port_2 = 3022") {
				t.Fatal("removal changed unrelated rules")
			}
			backups, _ := filepath.Glob(filepath.Join(firewallBackupDir(), "nftables.conf.bak.*"))
			if scenario == "not set" {
				if len(*calls) != 0 || len(backups) != 0 || saved != content {
					t.Fatal("absent configuration should not be written, backed up or loaded")
				}
			} else if !reflect.DeepEqual(*calls, []string{"check", "load"}) || len(backups) != 1 || shared.ReadFileString(backups[0]) != content {
				t.Fatal("removal must validate, preserve one original backup, and load once")
			}
			if err := configure(inputUI(t, "n\nn\n1\n")); err != nil {
				t.Fatal(err)
			}
			regenerated := shared.ReadFileString(configPath)
			if _, found, err := readPingSetting(regenerated); err != nil || found {
				t.Fatal("regeneration recreated the removed setting")
			}
			if rules, err := managedRules(regenerated); err != nil || !reflect.DeepEqual(rules, orderTestRules()) {
				t.Fatal("regeneration lost unrelated allow rules")
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
		if isSSHDTest(name, args) {
			return sshdTestConfig(2222), nil
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
	if err := Run(inputUI(t, "7\n2\n2\ny\n\nq\nq\n")); !errors.Is(err, shared.ErrReturnToMenu) {
		t.Fatal(err)
	}
	state, _, err = readPingSetting(shared.ReadFileString(configPath))
	if err != nil || state || !reflect.DeepEqual(*calls, []string{"check", "load", "check", "load"}) {
		t.Fatalf("menu did not save and load disabled Ping: %v %v", err, *calls)
	}
}
