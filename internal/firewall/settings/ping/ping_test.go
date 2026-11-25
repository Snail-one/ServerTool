package ping_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/firewall/configuration/generate"
	rulespkg "snail_tool/internal/firewall/internal/rules"
	"snail_tool/internal/firewall/internal/testenv"
	"snail_tool/internal/firewall/loadedrules"
	autostartconfig "snail_tool/internal/firewall/settings/autostart"
	"snail_tool/internal/firewall/settings/ping"
	"snail_tool/internal/firewall/settings/sshaccess"
	"snail_tool/internal/storage"
	"snail_tool/internal/ui"
)

func TestPingSwitchPreservesOtherRulesAndPrecedesConnectionTracking(t *testing.T) {
	base, err := generate.Build([]int{2222}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate an older configuration without a dedicated Ping setting.
	legacy := storage.RemoveManagedBlock(base, ping.Begin, ping.End)
	legacy, err = sshaccess.ReplacePorts(legacy, []int{2222, 3022})
	if err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{true, false, true} {
		updated, err := ping.Replace(legacy, enabled)
		if err != nil {
			t.Fatal(err)
		}
		state, found, err := ping.Read(updated)
		if err != nil || !found || state != enabled {
			t.Fatalf("incorrect setting: %v %v %v", state, found, err)
		}
		// Apart from the inserted block and whitespace, every existing rule stays.
		if !configuration.SameTokens(storage.RemoveManagedBlock(updated, ping.Begin, ping.End), legacy) {
			t.Fatal("Ping switch changed SSH, ports, control traffic or manual rules")
		}
		if strings.Index(updated, ping.Begin) > strings.Index(updated, "ct state established,related") {
			t.Fatal("Ping must precede rules that could bypass its setting")
		}
		// Repeated SSH port changes must remain compatible with the Ping block.
		for i := 0; i < 2; i++ {
			updated, err = sshaccess.ReplacePorts(updated, []int{2222, 4022})
			if err != nil {
				t.Fatal(err)
			}
		}
		state, _, err = ping.Read(updated)
		if err != nil || state != enabled || strings.Count(updated, ping.Begin) != 1 ||
			!strings.Contains(updated, "elements = { 2222, 4022 }") || strings.Contains(updated, sshaccess.LegacyBegin) {
			t.Fatal("SSH updates damaged or duplicated Ping settings")
		}
	}
}

func TestPingRefusesDamagedUnreachableAndManuallyChangedBlocks(t *testing.T) {
	base, _ := generate.Build([]int{2222}, false, false)
	for _, content := range []string{
		strings.Replace(base, ping.End, "", 1),
		base + "\n" + ping.Begin,
		strings.Replace(base, ping.Begin, "icmp type echo-request accept\n"+ping.Begin, 1),
		strings.Replace(base, "counter drop comment \"snail:ping\"", "counter accept comment \"snail:ping\"", 1),
		strings.Replace(base, "hook input", "hook forward", 1),
		strings.Replace(base, "icmpv6 type echo-request", "icmpv6 type nd-neighbor-solicit", 1),
	} {
		if _, err := ping.Replace(content, true); err == nil {
			t.Fatal("unsafe or ambiguous Ping configuration accepted")
		}
		if _, err := ping.Remove(content); err == nil {
			t.Fatal("damaged Ping configuration must not be deleted")
		}
	}
}

func TestConfigurePingValidationBackupLoadAndConcurrentEdits(t *testing.T) {
	for _, scenario := range []string{"on", "off", "invalid", "concurrent", "load failure"} {
		t.Run(scenario, func(t *testing.T) {
			env := testenv.New(t)
			version := env.RuleConfig(t, testenv.OrderRules())
			wantEnabled := scenario != "off"
			input := "1\ny\n"
			if !wantEnabled {
				on, _ := ping.Replace(string(version.Content), true)
				if err := os.WriteFile(env.ConfigPath, []byte(on), 0600); err != nil {
					t.Fatal(err)
				}
				version, _ = env.Store().Read()
				input = "2\ny\n"
			}
			var calls []string
			env.CommandOutput = func(name string, args ...string) (string, error) {
				if name == "nft" && reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
					return `{"nftables":[]}`, nil
				}
				if name == "nft" && len(args) == 3 && args[0] == "-c" && args[1] == "-f" {
					calls = append(calls, "check")
					if scenario == "invalid" {
						return "syntax error", errors.New("invalid")
					}
					if scenario == "concurrent" {
						if err := os.WriteFile(env.ConfigPath, []byte("# external edit\n"), 0600); err != nil {
							t.Fatal(err)
						}
					}
					return "", nil
				}
				if name == "nft" && reflect.DeepEqual(args, []string{"-f", env.ConfigPath}) {
					calls = append(calls, "load")
					if scenario == "load failure" {
						return "load failed", errors.New("failed")
					}
					return "", nil
				}
				t.Fatalf("unexpected command: %s %v", name, args)
				return "", nil
			}
			err := env.Ping().Run(testenv.Input(t, input))
			backups, _ := filepath.Glob(filepath.Join(env.BackupDir(), "nftables.conf.bak.*"))
			saved := storage.ReadFileString(env.ConfigPath)
			if scenario == "invalid" || scenario == "concurrent" {
				want := string(version.Content)
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
			if len(backups) != 1 || storage.ReadFileString(backups[0]) != string(version.Content) {
				t.Fatal("original configuration was not backed up exactly once")
			}
			state, found, err := ping.Read(saved)
			if err != nil || !found || state != wantEnabled {
				t.Fatal("incorrect saved Ping setting")
			}
			got, err := rulespkg.ManagedRules(saved)
			if err != nil || !reflect.DeepEqual(got, testenv.OrderRules()) {
				t.Fatal("Ping setting changed managed allow rules")
			}
		})
	}
}

func TestPingCancelDoesNotWriteOrConsumeNextInput(t *testing.T) {
	for _, input := range []string{"q\n", "返回\n", "1\nn\n", "2\n\n", "1\nq\nq\n", "3\nn\n", "3\nq\nq\n"} {
		t.Run(input, func(t *testing.T) {
			env := testenv.New(t)
			version := env.RuleConfig(t, nil)
			env.CommandOutput = func(name string, args ...string) (string, error) {
				if name == "nft" && reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
					return `{"nftables":[]}`, nil
				}
				t.Fatalf("unexpected query: %s %v", name, args)
				return "", nil
			}
			view := testenv.Input(t, input+"next\n")
			if err := env.Ping().Run(view); !errors.Is(err, ui.ErrReturnToMenu) {
				t.Fatalf("cancel did not return directly: %v", err)
			}
			if storage.ReadFileString(env.ConfigPath) != string(version.Content) {
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
			env := testenv.New(t)
			version := env.RuleConfig(t, testenv.OrderRules())
			content, err := ping.Replace(string(version.Content), scenario == "allowed")
			if scenario == "not set" {
				content, err = ping.Remove(content)
			}
			if err != nil {
				t.Fatal(err)
			}
			content, err = sshaccess.ReplacePorts(content, []int{2222, 3022})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(env.ConfigPath, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			calls := env.AcceptRuleCommands(t)
			accept := env.CommandOutput
			env.CommandOutput = func(name string, args ...string) (string, error) {
				if name == "nft" && reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
					return `{"nftables":[]}`, nil
				}
				if testenv.IsSSHDQuery(name, args) {
					return testenv.SSHDConfig(2222), nil
				}
				return accept(name, args...)
			}
			input := "3\ny\nnext\n"
			if scenario == "not set" {
				input = "3\nnext\n"
			}
			view := testenv.Input(t, input)
			if err := env.Ping().Run(view); err != nil {
				t.Fatal(err)
			}
			if next, err := view.Ask("next"); err != nil || next != "next" {
				t.Fatal("removal consumed an unexpected confirmation or pause")
			}
			saved := storage.ReadFileString(env.ConfigPath)
			if strings.Contains(saved, "snail:ping") || strings.Contains(saved, ping.Begin) || strings.Contains(saved, ping.End) {
				t.Fatal("dedicated Ping rules or markers remain")
			}
			// Every other active directive, including SSH, is preserved.
			without, _ := ping.Remove(content)
			if !configuration.SameTokens(saved, without) || !strings.Contains(saved, "elements = { 2222, 3022 }") {
				t.Fatal("removal changed unrelated rules")
			}
			backups, _ := filepath.Glob(filepath.Join(env.BackupDir(), "nftables.conf.bak.*"))
			if scenario == "not set" {
				if len(*calls) != 0 || len(backups) != 0 || saved != content {
					t.Fatal("absent configuration should not be written, backed up or loaded")
				}
			} else if !reflect.DeepEqual(*calls, []string{"check", "load"}) || len(backups) != 1 || storage.ReadFileString(backups[0]) != content {
				t.Fatal("removal must validate, preserve one original backup, and load once")
			}
			if err := env.Generate(testenv.Input(t, "\n\n\n\n1\n")); err != nil {
				t.Fatal(err)
			}
			regenerated := storage.ReadFileString(env.ConfigPath)
			if _, found, err := ping.Read(regenerated); err != nil || found {
				t.Fatal("regeneration recreated the removed setting")
			}
			want, _ := generate.OmitUnconvertedPortRules(testenv.OrderRules())
			if rules, err := rulespkg.ManagedRules(regenerated); err != nil || !reflect.DeepEqual(rules, want) {
				t.Fatal("regeneration lost unrelated allow rules")
			}
		})
	}
}

func TestRegenerationKeepsPingEnabledAndMenuEntryWorks(t *testing.T) {
	env := testenv.New(t)
	version := env.RuleConfig(t, nil)
	enabled, _ := ping.Replace(string(version.Content), true)
	if err := os.WriteFile(env.ConfigPath, []byte(enabled), 0600); err != nil {
		t.Fatal(err)
	}
	calls := env.AcceptRuleCommands(t)
	accept := env.CommandOutput
	env.CommandOutput = func(name string, args ...string) (string, error) {
		if testenv.IsSSHDQuery(name, args) {
			return testenv.SSHDConfig(2222), nil
		}
		if name == "nft" && reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
			return `{"nftables":[]}`, nil
		}
		return accept(name, args...)
	}
	if err := env.Generate(testenv.Input(t, "\n\n\n\n1\n")); err != nil {
		t.Fatal(err)
	}
	state, _, err := ping.Read(storage.ReadFileString(env.ConfigPath))
	if err != nil || !state {
		t.Fatal("regeneration lost enabled Ping")
	}
	if err := env.Run(testenv.Input(t, "8\n2\n2\ny\n\nq\nq\n")); !errors.Is(err, ui.ErrReturnToMenu) {
		t.Fatal(err)
	}
	state, _, err = ping.Read(storage.ReadFileString(env.ConfigPath))
	if err != nil || state || !reflect.DeepEqual(*calls, []string{"check", "load", "check", "load"}) {
		t.Fatalf("menu did not save and load disabled Ping: %v %v", err, *calls)
	}
}

func echoTestRule(chain, expressions string) string {
	return fmt.Sprintf(`{"rule":{"family":"inet","table":"snail","chain":%q,"expr":[%s]}}`, chain, expressions)
}

func echoTestRules(objects ...string) string {
	return `{"nftables":[{"table":{"family":"inet","name":"snail"}},` +
		`{"chain":{"family":"inet","table":"snail","name":"input","type":"filter","hook":"input","policy":"drop"}},` +
		strings.Join(objects, ",") + `]}`
}

func TestLoadedEchoReadsActualRulesAndTheirOrder(t *testing.T) {
	for _, ipv6 := range []bool{false, true} {
		want := ping.EchoBlock
		if !ipv6 {
			// This fixture includes a manual all-protocol IPv4 source exception.
			want |= ping.EchoAllow
		}
		if state := ping.LoadedEchoState(testenv.PortRules(), ipv6); state != want {
			t.Fatalf("source exceptions in legacy fixture were lost: ipv6=%v state=%d", ipv6, state)
		}
	}
	request4 := `{"match":{"op":"==","left":{"payload":{"protocol":"icmp","field":"type"}},"right":"echo-request"}}`
	request6 := strings.Replace(request4, `"icmp"`, `"icmpv6"`, 1)
	loopback := `{"match":{"op":"==","left":{"meta":{"key":"iifname"}},"right":"lo"}}`
	established := `{"match":{"op":"in","left":{"ct":{"key":"state"}},"right":{"set":["established","related"]}}}`
	source := `{"match":{"op":"==","left":{"payload":{"protocol":"ip","field":"saddr"}},"right":"192.0.2.1"}}`
	for _, scenario := range []struct {
		name   string
		rules  []string
		v4, v6 int
	}{
		{"both allowed", []string{echoTestRule("input", request4+`,{"accept":null}`), echoTestRule("input", request6+`,{"accept":null}`)}, ping.EchoAllow, ping.EchoAllow},
		{"both blocked", []string{echoTestRule("input", `{"drop":null}`)}, ping.EchoBlock, ping.EchoBlock},
		{"families differ", []string{echoTestRule("input", request4+`,{"accept":null}`)}, ping.EchoAllow, ping.EchoBlock},
		{"earlier drop wins", []string{echoTestRule("input", `{"drop":null}`), echoTestRule("input", request4+`,{"accept":null}`)}, ping.EchoBlock, ping.EchoBlock},
		{"loopback is not external", []string{echoTestRule("input", loopback+`,{"accept":null}`)}, ping.EchoBlock, ping.EchoBlock},
		{"established does not allow new Ping", []string{echoTestRule("input", established+`,{"accept":null}`)}, ping.EchoBlock, ping.EchoBlock},
		{"control messages only", []string{echoTestRule("input", strings.Replace(request6, "echo-request", "nd-neighbor-solicit", 1)+`,{"accept":null}`)}, ping.EchoBlock, ping.EchoBlock},
		{"conditional source", []string{echoTestRule("input", source+`,`+request4+`,{"accept":null}`)}, ping.EchoAllow | ping.EchoBlock, ping.EchoBlock},
		{"unsupported rate limit", []string{echoTestRule("input", request4+`,{"limit":{"rate":5}},{"accept":null}`)}, ping.EchoUnknown, ping.EchoBlock},
		{"jump returns to fallback", []string{`{"chain":{"family":"inet","table":"snail","name":"extra"}}`, echoTestRule("input", `{"jump":{"target":"extra"}}`), echoTestRule("extra", `{"return":null}`)}, ping.EchoBlock, ping.EchoBlock},
		{"jump accepts", []string{`{"chain":{"family":"inet","table":"snail","name":"extra"}}`, echoTestRule("input", `{"jump":{"target":"extra"}}`), echoTestRule("extra", request4+`,{"accept":null}`)}, ping.EchoAllow, ping.EchoBlock},
		{"cycle is unknown", []string{echoTestRule("input", `{"jump":{"target":"input"}}`)}, ping.EchoUnknown, ping.EchoUnknown},
		{"other input hook is unknown", []string{`{"chain":{"family":"inet","table":"snail","name":"extra","hook":"input"}}`}, ping.EchoUnknown, ping.EchoUnknown},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			output := echoTestRules(scenario.rules...)
			if v4, v6 := ping.LoadedEchoState(output, false), ping.LoadedEchoState(output, true); v4 != scenario.v4 || v6 != scenario.v6 {
				t.Fatalf("states=(%d,%d) want=(%d,%d)", v4, v6, scenario.v4, scenario.v6)
			}
		})
	}
}

func TestEchoCardsSeparateSavedSwitchFromLoadedBlocking(t *testing.T) {
	env := testenv.New(t)
	version := env.RuleConfig(t, nil)
	allowed, _ := ping.Replace(string(version.Content), true)
	if err := os.WriteFile(env.ConfigPath, []byte(allowed), 0600); err != nil {
		t.Fatal(err)
	}
	// File allows Ping, but the loaded input chain still drops it.
	loaded := echoTestRules(echoTestRule("input", `{"drop":null}`))
	env.CommandOutput = func(name string, args ...string) (string, error) {
		if name != "nft" || !reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
			t.Fatalf("unexpected query: %s %v", name, args)
		}
		return loaded, nil
	}
	fields := env.StatusFields(autostartconfig.Status{})
	if testenv.FieldValue(fields, "ICMP 回显") != "已拦截" || testenv.FieldValue(fields, "ICMPv6 回显") != "已拦截" {
		t.Fatal("main card inferred loaded state from saved switch")
	}
	output := testenv.CaptureOutput(t, func() {
		if err := env.Ping().Run(testenv.Input(t, "q\n")); err == nil {
			t.Fatal("expected return sentinel")
		}
	})
	for _, want := range []string{"ICMP 回显：已拦截", "保存的开关：已允许", "配置同步：无法比较"} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %q in card: %s", want, output)
		}
	}
	for _, removed := range []string{"作用范围：", "保留流量：", "生效方式：", "连通条件："} {
		if strings.Contains(output, removed) {
			t.Fatalf("static explanation still in compact card: %s", removed)
		}
	}
	for _, live := range []loadedrules.Snapshot{{}, {Err: errors.New("denied")}} {
		for _, field := range ping.StateFields(true, live.InputActive, live.JSON, live.Err) {
			if field.Value == "已允许" || field.Value == "已拦截" {
				t.Fatal("inactive or unreadable firewall was assigned an active verdict")
			}
		}
	}
}
