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

func assertNoPacketCounters(t *testing.T, content string) {
	t.Helper()
	tokens, err := configTokens(content)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range tokens {
		if token.text == "counter" {
			t.Fatal("statistics are disabled but an active counter remains")
		}
	}
}

func TestPacketCountersOnlyChangeStatisticsInManagedTable(t *testing.T) {
	base, _ := buildConfig([]int{2222}, true, false)
	rules := orderTestRules()
	base, _ = replaceManagedRules(base, rules)
	// Include manual rules, initialized counters, multiline sets and strings
	// containing keywords. Other tables and text outside the block stay exact.
	manual := "tcp dport 8080 counter packets 12 bytes 345 reject with tcp reset comment \"counter accept\"\n        "
	base = strings.Replace(base, "        return\n", "        "+manual+"return\n", 1)
	base = strings.Replace(base, "counter drop\n", "counter packets 7 bytes 42 drop\n", 1)
	other := "\ntable ip other { chain input { tcp dport 90 counter accept; } }\n"
	base = strings.Replace(base, configEnd, other+configEnd, 1)
	prefix, suffix := "# counter accept: keep this comment\n", "\n# keep tail\n"
	base = prefix + base + suffix
	off, err := withPacketCounters(base, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(off, prefix) || !strings.HasSuffix(off, suffix) || !strings.Contains(off, other) || !strings.Contains(off, `comment "counter accept"`) {
		t.Fatal("unrelated table, comment, or quoted text changed")
	}
	before, _ := withoutCounterStatements(base)
	after, _ := withoutCounterStatements(off)
	if !sameConfigTokens(before, after) {
		t.Fatal("turning counters off changed a match, verdict, or policy")
	}
	owned := strings.Replace(off, other, "", 1)
	assertNoPacketCounters(t, owned)
	if got, err := managedRules(off); err != nil || !reflect.DeepEqual(got, rules) {
		t.Fatalf("rule identities or order changed: %v %v", got, err)
	}
	on, err := withPacketCounters(off, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(on, "policy drop;") || strings.Contains(on, "policy counter") || !strings.Contains(on, "counter reject with tcp reset") || !strings.Contains(on, other) {
		t.Fatal("enabling statistics damaged a policy, verdict or other table")
	}
	_, total, counted, err := rewritePacketCounters(on, true)
	if err != nil || total == 0 || counted != total {
		t.Fatalf("not all verdict rules counted: %d/%d %v", counted, total, err)
	}
	again, err := withPacketCounters(on, true)
	if err != nil || again != on || strings.Contains(on, "counter counter") {
		t.Fatal("repeated enabling duplicates or changes counters")
	}
	after, _ = withoutCounterStatements(on)
	if !sameConfigTokens(before, after) {
		t.Fatal("enabling statistics changed firewall behavior")
	}
}

func TestPacketCountersRejectAmbiguousConfigurations(t *testing.T) {
	base, _ := buildConfig([]int{2222}, false, false)
	for name, content := range map[string]string{
		"missing block":         strings.Replace(base, configEnd, "", 1),
		"bad setting":           strings.Replace(base, configBegin, configBegin+"\n"+packetCounterMarker+"invalid", 1),
		"duplicate setting":     strings.Replace(base, configBegin, configBegin+"\n"+packetCounterMarker+"enabled\n"+packetCounterMarker+"disabled", 1),
		"setting outside block": base + "\n" + packetCounterMarker + "disabled\n",
		"named counter":         strings.Replace(base, "counter accept", "counter name manual accept", 1),
		"invalid initial value": strings.Replace(base, "counter accept", "counter packets unknown accept", 1),
		"duplicate table":       strings.Replace(base, configEnd, "table inet snail {}\n"+configEnd, 1),
		"managed rule drift":    strings.Replace(base, pingBegin, "icmp type echo-request accept\n"+pingBegin, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := withPacketCounters(content, false); err == nil {
				t.Fatal("unsafe configuration accepted")
			}
		})
	}
}

func TestPacketCounterPreferenceSurvivesRulePingAndSSHUpdates(t *testing.T) {
	base, _ := buildConfig([]int{2222}, false, false)
	if enabled, err := packetCountersEnabled(base); err != nil || !enabled {
		t.Fatal("legacy configuration must default to counters on")
	}
	content, err := withPacketCounters(base, false)
	if err != nil {
		t.Fatal(err)
	}
	rules := orderTestRules()
	// Cover additions, replacement/editing, reordering and deletion.
	for _, wanted := range [][]allowRule{rules, {rules[3], rules[0]}, nil} {
		content, err = replaceManagedRules(content, wanted)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := managedRules(content); err != nil || !reflect.DeepEqual(got, wanted) && !(len(got) == 0 && len(wanted) == 0) {
			t.Fatalf("incorrect managed rules: %v %v", got, err)
		}
		assertNoPacketCounters(t, content)
	}
	for _, enabled := range []bool{true, false} {
		content, err = withPingSetting(content, enabled)
		if err != nil {
			t.Fatal(err)
		}
		if got, found, err := readPingSetting(content); err != nil || !found || got != enabled {
			t.Fatalf("incorrect Ping setting: %v %v %v", got, found, err)
		}
		content, err = withSSHPorts(content, []int{2222, 3022})
		if err != nil {
			t.Fatal(err)
		}
		assertNoPacketCounters(t, content)
	}
	content, err = withoutPingSetting(content)
	if err != nil {
		t.Fatal(err)
	}
	assertNoPacketCounters(t, content)
	if enabled, err := packetCountersEnabled(content); err != nil || enabled {
		t.Fatal("packet counter preference was lost")
	}
}

func TestConfigurePacketCountersValidatesBacksUpAndLoads(t *testing.T) {
	for _, scenario := range []string{"off", "on", "invalid", "concurrent", "load failure"} {
		t.Run(scenario, func(t *testing.T) {
			setupTest(t)
			version := ruleTestConfig(t, orderTestRules())
			input, enabled := "2\ny\n", false
			if scenario == "on" {
				content, err := withPacketCounters(string(version.content), false)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
				version, _ = readConfigVersion()
				input, enabled = "1\ny\n", true
			}
			var calls []string
			commandOutput = func(name string, args ...string) (string, error) {
				if name == "nft" && len(args) == 3 && args[0] == "-c" && args[1] == "-f" {
					calls = append(calls, "check")
					proposed := shared.ReadFileString(args[2])
					if value, err := packetCountersEnabled(proposed); err != nil || value != enabled {
						t.Fatal("wrong proposed packet counter setting")
					}
					if !enabled {
						assertNoPacketCounters(t, proposed)
					}
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
			err := configurePacketCounters(inputUI(t, input))
			backups, _ := filepath.Glob(filepath.Join(firewallBackupDir(), "nftables.conf.bak.*"))
			saved := shared.ReadFileString(configPath)
			if scenario == "invalid" || scenario == "concurrent" {
				wanted := string(version.content)
				if scenario == "concurrent" {
					wanted = "# external edit\n"
				}
				if err == nil || saved != wanted || len(backups) != 0 || !reflect.DeepEqual(calls, []string{"check"}) {
					t.Fatalf("failed operation wrote or loaded config: %v %v", err, calls)
				}
				return
			}
			if (err != nil) != (scenario == "load failure") || !reflect.DeepEqual(calls, []string{"check", "load"}) || len(backups) != 1 || shared.ReadFileString(backups[0]) != string(version.content) {
				t.Fatalf("incorrect validation/backup/load: %v %v %v", err, calls, backups)
			}
			if scenario == "load failure" && !strings.Contains(err.Error(), "未成功加载") {
				t.Fatal("load failure claimed success")
			}
			if got, err := managedRules(saved); err != nil || !reflect.DeepEqual(got, orderTestRules()) {
				t.Fatal("packet counter switch changed rule identities or order")
			}
		})
	}
}

func TestPacketCounterCancelReturnsWithoutWritingOrExtraPrompt(t *testing.T) {
	for _, input := range []string{"q\n", "返回\n", "1\nn\n", "2\nq\n"} {
		t.Run(input, func(t *testing.T) {
			setupTest(t)
			version := ruleTestConfig(t, nil)
			view := inputUI(t, input+"next\n")
			if err := configurePacketCounters(view); !errors.Is(err, shared.ErrReturnToMenu) {
				t.Fatal(err)
			}
			if shared.ReadFileString(configPath) != string(version.content) {
				t.Fatal("cancel modified the file")
			}
			if next, err := view.Ask("next"); err != nil || next != "next" {
				t.Fatal("cancel consumed extra input")
			}
		})
	}
}

func TestRegenerationRetainsDisabledCounters(t *testing.T) {
	for _, removePing := range []bool{false, true} {
		t.Run(map[bool]string{false: "ping allowed", true: "ping not set"}[removePing], func(t *testing.T) {
			setupTest(t)
			version := ruleTestConfig(t, orderTestRules())
			content, _ := withPacketCounters(string(version.content), false)
			var err error
			if removePing {
				content, err = withoutPingSetting(content)
			} else {
				content, err = withPingSetting(content, true)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			acceptRuleCommands(t)
			accept := commandOutput
			commandOutput = func(name string, args ...string) (string, error) {
				if name == "ss" {
					return `LISTEN 0 128 *:2222 *:* users:(("sshd",pid=1,fd=3))`, nil
				}
				return accept(name, args...)
			}
			if err := configure(inputUI(t, "\n\n1\n")); err != nil {
				t.Fatal(err)
			}
			saved := shared.ReadFileString(configPath)
			assertNoPacketCounters(t, saved)
			if value, err := packetCountersEnabled(saved); err != nil || value {
				t.Fatal("regeneration forgot the disabled preference")
			}
			if value, found, err := readPingSetting(saved); err != nil || found == removePing || !removePing && !value {
				t.Fatal("regeneration changed Ping behavior")
			}
		})
	}
}

func TestSSHLivePortUpdateHonorsDisabledCounters(t *testing.T) {
	setupTest(t)
	version := ruleTestConfig(t, nil)
	content, err := withPacketCounters(string(version.content), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	loaded := false
	commandOutput = func(name string, args ...string) (string, error) {
		switch {
		case name == "ss":
			return `LISTEN 0 128 *:2222 *:* users:(("sshd",pid=1,fd=3))`, nil
		case name == "nft" && reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}):
			return activeSSHFirewall, nil
		case name == "nft" && len(args) == 3 && args[0] == "-c" && args[1] == "-f":
			assertNoPacketCounters(t, shared.ReadFileString(args[2]))
			return "", nil
		case name == "nft" && len(args) == 2 && args[0] == "-f":
			if args[1] == configPath {
				t.Fatal("SSH update must use a small live transaction")
			}
			batch := shared.ReadFileString(args[1])
			assertNoPacketCounters(t, batch)
			if !strings.Contains(batch, "tcp dport { 2222, 3022 } accept") || !strings.Contains(batch, "udp dport { 2222, 3022 } accept") {
				t.Fatal("current/new SSH listeners not preserved")
			}
			loaded = true
			return "", nil
		default:
			t.Fatalf("unexpected command: %s %v", name, args)
			return "", nil
		}
	}
	if active, err := EnsureSSHPorts([]int{3022}); err != nil || !active || !loaded {
		t.Fatalf("SSH update failed: %v %v", active, err)
	}
	assertNoPacketCounters(t, shared.ReadFileString(configPath))
}
