package firewall

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/firewall/configuration/generate"
	rulespkg "snail_tool/internal/firewall/internal/rules"
	"snail_tool/internal/storage"
	"snail_tool/internal/ui"
)

func acceptRuleCommands(t *testing.T) *[]string {
	t.Helper()
	var calls []string
	commandOutput = func(name string, args ...string) (string, error) {
		if name != "nft" {
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		if len(args) == 3 && args[0] == "-c" && args[1] == "-f" {
			calls = append(calls, "check")
			if _, err := rulespkg.ManagedRules(storage.ReadFileString(args[2])); err != nil {
				t.Fatalf("invalid rule block: %v", err)
			}
			return "", nil
		}
		if reflect.DeepEqual(args, []string{"-f", configPath}) {
			calls = append(calls, "load")
			return "", nil
		}
		t.Fatalf("unexpected nft arguments: %v", args)
		return "", nil
	}
	return &calls
}

func TestRuleManagerAddsSourceExceptionForBothProtocols(t *testing.T) {
	setupTest(t)
	ruleTestConfig(t, nil)
	calls := acceptRuleCommands(t)
	// Unified draft -> source exceptions -> add -> both IP versions and protocols.
	if err := runAccessRuleManager(inputUI(t, "5\nn\n\n3\n192.168.1.0/24\nfd12::/48\n\n\n\n\n8000-8100\n3\n\nq\ns\n\nq\n")); !errors.Is(err, ui.ErrReturnToMenu) {
		t.Fatal(err)
	}
	rules, err := rulespkg.ManagedRules(storage.ReadFileString(configPath))
	if err != nil || len(rules) != 1 || rules[0].Kind != "source_port" || rules[0].Family != "both" || rules[0].Ports != "8000-8100" || rules[0].Protocol != "both" {
		t.Fatalf("incorrect saved rule: %v %v", rules, err)
	}
	if !reflect.DeepEqual(*calls, []string{"check", "load"}) {
		t.Fatalf("rule must validate and load once: %v", *calls)
	}
}

func TestEditRuleKeepsIDAndCanonicalizesCIDR(t *testing.T) {
	setupTest(t)
	initial := []rulespkg.Rule{{ID: "aaaaaaaa", Kind: "source_port", Source: "192.168.1.0/24", Ports: "443", Protocol: "tcp"}}
	version := ruleTestConfig(t, initial)
	rules, err := accessPolicy().ChangeAllowRuleDraft(inputUI(t, "\n\n192.168.1.1/16\n\n\n\n\n21069\n3\n"), initial, "aaaaaaaa")
	if storage.ReadFileString(configPath) != string(version.Content) {
		t.Fatal("editing a draft wrote the configuration")
	}
	want := []rulespkg.Rule{{ID: "aaaaaaaa", Kind: "source_port", Family: "ipv4", Source: "192.168.0.0/16", Ports: "21069", Protocol: "both"}}
	if err != nil || !reflect.DeepEqual(rules, want) {
		t.Fatalf("editing changed ID or failed to normalize: %v %v", rules, err)
	}
}

func TestRuleManagerEditsByDisplayedNumberAndKeepsOtherRules(t *testing.T) {
	setupTest(t)
	rules := orderTestRules()
	ruleTestConfig(t, rules)
	calls := acceptRuleCommands(t)
	// Edit row 2, keep its type/source, and change its ports/protocol.
	if err := runAccessRuleManager(inputUI(t, "5\n2\n1\n\n\n\n\n\n\n\n8443\n3\n\nq\nq\ns\n\nq\n")); !errors.Is(err, ui.ErrReturnToMenu) {
		t.Fatal(err)
	}
	want := append([]rulespkg.Rule(nil), rules...)
	want[1].Ports, want[1].Protocol = "8443", "both"
	want[1].Family = "ipv4"
	got, err := rulespkg.ManagedRules(storage.ReadFileString(configPath))
	if err != nil || !reflect.DeepEqual(got, want) || !reflect.DeepEqual(*calls, []string{"check", "load"}) {
		t.Fatalf("editing by number changed the wrong rule: got=%v err=%v calls=%v", got, err, *calls)
	}
}

func TestChooseRuleAcceptsNumbersAndPrefersFullNumericIDs(t *testing.T) {
	rules := []rulespkg.Rule{{ID: "aaaaaaaa"}, {ID: "00000001"}}
	for _, scenario := range []struct{ input, want string }{
		{"1\n", "aaaaaaaa"},
		{"2\n", "00000001"},
		{"00000001\n", "00000001"},
		{"AAAAAAAA\n", "aaaaaaaa"},
		{"3\nno-such-id\n-1\n2\n", "00000001"},
	} {
		t.Run(scenario.input, func(t *testing.T) {
			view := inputUI(t, scenario.input+"next\n")
			id, err := rulespkg.ChooseRuleID(view, rules)
			if err != nil || id != scenario.want {
				t.Fatalf("id=%q want=%q err=%v", id, scenario.want, err)
			}
			if next, err := view.Ask("next"); err != nil || next != "next" {
				t.Fatal("selection consumed extra input")
			}
		})
	}
}

func TestDeleteByNumberUsesTheDisplayedFilteredList(t *testing.T) {
	setupTest(t)
	rules := []rulespkg.Rule{
		{ID: "aaaaaaaa", Kind: "port", Ports: "443", Protocol: "tcp"},
		{ID: "bbbbbbbb", Kind: "port", Ports: "8080", Protocol: "tcp"},
		{ID: "cccccccc", Kind: "port", Ports: "443", Protocol: "udp"},
	}
	version := ruleTestConfig(t, rules)
	// The second matching row is rule C, which was third in the full list.
	got, err := accessPolicy().DeleteRuleByPortDraft(inputUI(t, "443\n2\n"), rules)
	if storage.ReadFileString(configPath) != string(version.Content) {
		t.Fatal("deleting from a draft wrote the configuration")
	}
	if err != nil || !reflect.DeepEqual(got, rules[:2]) {
		t.Fatalf("filtered number selected a rule from the original list: got=%v err=%v", got, err)
	}
}

func TestRuleAddCancellationAndDuplicateDoNotChangeConfiguration(t *testing.T) {
	for _, scenario := range []string{"cancel ports", "duplicate", "cancel family", "cancel permissions", "cancel second source"} {
		t.Run(scenario, func(t *testing.T) {
			setupTest(t)
			rules := []rulespkg.Rule{{ID: "aaaaaaaa", Kind: "source_port", Source: "192.168.0.0/16", Ports: "443", Protocol: "tcp"}}
			version := ruleTestConfig(t, rules)
			input := "\n1\n192.168.1.10\n\n\n\n\nq\n"
			if scenario == "duplicate" {
				input = "\n1\n192.168.1.1/16\n\n\n\n\n443-443\n1\n"
			} else if scenario == "cancel family" {
				input = "q\n"
			} else if scenario == "cancel permissions" {
				input = "\n1\n192.168.1.10\nq\n"
			} else if scenario == "cancel second source" {
				input = "\n3\n192.168.1.0/24\nq\n"
			}
			_, err := accessPolicy().ChangeAllowRuleDraft(inputUI(t, input), rules, "")
			if scenario == "duplicate" && (err == nil || !strings.Contains(err.Error(), "完全重复")) {
				t.Fatalf("normalized duplicate not rejected: %v", err)
			}
			if storage.ReadFileString(configPath) != string(version.Content) {
				t.Fatal("cancelled or duplicate addition changed the file")
			}
		})
	}
}

func TestQueryPortAndDeleteOnlyChosenIDPreservingOtherSources(t *testing.T) {
	setupTest(t)
	rules := []rulespkg.Rule{
		{ID: "aaaaaaaa", Kind: "port", Ports: "443", Protocol: "tcp"},
		{ID: "bbbbbbbb", Kind: "source_port", Source: "192.168.1.0/24", Ports: "443", Protocol: "tcp"},
		{ID: "cccccccc", Kind: "source", Source: "10.0.0.0/8", Protocol: "both"},
		{ID: "dddddddd", Kind: "port", Ports: "8000-8100", Protocol: "udp"},
	}
	version := ruleTestConfig(t, rules)
	// All-port source rules are included because they also allow the queried port.
	matches, err := accessPolicy().QueryRulesByPort(inputUI(t, "443\n"), rules)
	if err != nil || !reflect.DeepEqual(matches, rules[:3]) {
		t.Fatalf("query must list each matching rule independently: %v %v", matches, err)
	}
	rangeMatches, err := accessPolicy().QueryRulesByPort(inputUI(t, "8080\n"), rules)
	if err != nil || len(rangeMatches) != 2 || rangeMatches[1].ID != "dddddddd" {
		t.Fatalf("port inside range not matched: %v %v", rangeMatches, err)
	}
	// dddddddd is not in the query result, so it must not be selected/deleted.
	got, err := accessPolicy().DeleteRuleByPortDraft(inputUI(t, "443\ndddddddd\nbbbbbbbb\n"), rules)
	if storage.ReadFileString(configPath) != string(version.Content) {
		t.Fatal("deleting from a draft wrote the configuration")
	}
	want := []rulespkg.Rule{rules[0], rules[2], rules[3]}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("deleting by port removed unrelated/global/source rules: %v %v", got, err)
	}
}

func TestNewDropRuleIsInsertedAheadOfAllows(t *testing.T) {
	existing, err := rulespkg.NormalizeAllowRule(rulespkg.Rule{ID: "aaaaaaaa", Kind: "source_port", Family: "ipv4", Source: "192.168.1.10", Ports: "443", Protocol: "tcp"})
	if err != nil {
		t.Fatal(err)
	}
	rules, err := accessPolicy().ChangeAllowRuleDraft(inputUI(t, "2\n1\n203.0.113.8\n\neth0\n\n\n22\n1\n"), []rulespkg.Rule{existing}, "")
	if err != nil || len(rules) != 2 || rules[0].Verdict != "drop" || rules[0].Kind != "rule" || rules[0].Iif != "eth0" || rules[1].ID != "aaaaaaaa" {
		t.Fatalf("drop was not inserted first: %v %v", rules, err)
	}
	rules, err = accessPolicy().ChangeAllowRuleDraft(inputUI(t, "\n1\n192.168.1.20\n\n\n\n\n3306\n1\n"), rules, "")
	if err != nil || len(rules) != 3 || rules[0].Verdict != "drop" || rules[2].Kind != "source_port" || rules[2].Ports != "3306" {
		t.Fatalf("allow was not appended after the drop: %v %v", rules, err)
	}
}

func TestDeleteByIDStaysInDraftUntilSaved(t *testing.T) {
	setupTest(t)
	rules := []rulespkg.Rule{{ID: "aaaaaaaa", Kind: "port", Ports: "443", Protocol: "tcp"}}
	version := ruleTestConfig(t, rules)
	if err := runAccessRuleManager(inputUI(t, "5\naaaaaaaa\n2\n\nq\nq\ny\n")); !errors.Is(err, ui.ErrReturnToMenu) {
		t.Fatal(err)
	}
	if storage.ReadFileString(configPath) != string(version.Content) {
		t.Fatal("cancelled deletion changed configuration")
	}
}

func TestRuleFamiliesRenderAndRoundTripWithoutBroadeningTraffic(t *testing.T) {
	for _, kind := range []string{"port", "source", "source_port"} {
		for _, family := range []string{"ipv4", "ipv6", "both"} {
			t.Run(kind+"/"+family, func(t *testing.T) {
				rule := rulespkg.Rule{ID: "1234abcd", Kind: kind, Family: family, Protocol: "both"}
				if kind != "source" {
					rule.Ports = "8000-8100"
				}
				if kind != "port" {
					rule.Source = "192.168.1.1/16"
					if family == "ipv6" {
						rule.Source = "2001:db8::1/32"
					} else if family == "both" {
						rule.SourceIPv6 = "2001:db8::1/32"
					}
				}
				rule, err := rulespkg.NormalizeAllowRule(rule)
				if err != nil {
					t.Fatal(err)
				}
				lines := rule.NftLines()
				wantLines := 2
				if kind != "port" && family == "both" {
					wantLines = 4
				}
				if len(lines) != wantLines {
					t.Fatalf("generated %d rules, want %d", len(lines), wantLines)
				}
				for _, line := range lines {
					if kind == "port" && family != "both" && !strings.HasPrefix(line, "meta nfproto "+family+" ") {
						t.Fatalf("port rule broadened to other IP versions: %s", line)
					}
					if kind != "port" && family == "ipv4" && !strings.HasPrefix(line, "ip saddr 192.168.0.0/16 ") {
						t.Fatal("IPv4 source lost", line)
					}
					if kind != "port" && family == "ipv6" && !strings.HasPrefix(line, "ip6 saddr 2001:db8::/32 ") {
						t.Fatal("IPv6 source lost", line)
					}
					if kind != "port" && family == "both" && !strings.HasPrefix(line, "ip saddr 192.168.0.0/16 ") && !strings.HasPrefix(line, "ip6 saddr 2001:db8::/32 ") {
						t.Fatal("dual-stack source constraint lost", line)
					}
				}
				base, _ := generate.Build([]int{2222}, false, false)
				content, err := rulespkg.ReplaceManagedRules(base, []rulespkg.Rule{rule})
				if err != nil {
					t.Fatal(err)
				}
				got, err := rulespkg.ManagedRules(content)
				if err != nil || !reflect.DeepEqual(got, []rulespkg.Rule{rule}) {
					t.Fatalf("IP version or source lost on reload: %v %v", got, err)
				}
			})
		}
	}
}

func TestAskRuleSupportsSeparateAndCombinedIPVersions(t *testing.T) {
	for _, test := range []struct{ input, family, source, source6 string }{
		{"3\n1\n443\n1\n", "ipv4", "", ""},
		{"3\n2\n443\n1\n", "ipv6", "", ""},
		{"3\n3\n443\n1\n", "both", "", ""},
		{"\n2\n192.168.1.1\n2001:db8::1\n\n\n\n\n443\n1\n", "ipv6", "2001:db8::1", ""},
		{"\n3\n192.168.1.1/16\n2001:db8::1/32\n\n\n\n\n443\n1\n", "both", "192.168.0.0/16", "2001:db8::/32"},
	} {
		current := rulespkg.Rule{ID: "1234abcd"}
		if test.source == "" {
			current.Kind, current.Ports, current.Protocol = "port", "443", "tcp"
		}
		rule, err := accessPolicy().AskAllowRule(inputUI(t, test.input), current)
		if err != nil || rule.Family != test.family || rule.Source != test.source || rule.SourceIPv6 != test.source6 {
			t.Fatalf("wrong family/source after input: %+v %v", rule, err)
		}
	}
}

func TestEditDualStackSourcesToOneVersionKeepsTheChosenAddress(t *testing.T) {
	current := rulespkg.Rule{ID: "1234abcd", Kind: "source_port", Family: "both", Source: "192.168.0.0/16", SourceIPv6: "2001:db8::/32", Ports: "443", Protocol: "tcp"}
	rule, err := accessPolicy().AskAllowRule(inputUI(t, "\n2\n\n\n\n\n\n\n\n"), current)
	if err != nil || rule.Family != "ipv6" || rule.Source != current.SourceIPv6 || rule.SourceIPv6 != "" {
		t.Fatalf("switching to IPv6 kept the wrong source: %+v %v", rule, err)
	}
}

func TestDualStackSourceRuleIsSavedMovedQueriedAndDeletedAsOneID(t *testing.T) {
	setupTest(t)
	other := rulespkg.Rule{ID: "aaaaaaaa", Kind: "port", Family: "ipv4", Ports: "53", Protocol: "udp"}
	ruleTestConfig(t, []rulespkg.Rule{other})
	acceptRuleCommands(t)
	if err := runAccessRuleManager(inputUI(t, "5\nn\n\n3\n192.168.1.1/16\n2001:db8::1/32\n\n\n\n\n443\n3\n\nq\ns\n\nq\n")); !errors.Is(err, ui.ErrReturnToMenu) {
		t.Fatal(err)
	}
	rules, err := rulespkg.ManagedRules(storage.ReadFileString(configPath))
	if err != nil || len(rules) != 2 || rules[1].Family != "both" || len(rules[1].NftLines()) != 4 {
		t.Fatalf("dual-stack sources were not stored as one logical rule: %v %v", rules, err)
	}
	target := rules[1]
	output := capturePortOutput(t, func() { rulespkg.PrintRuleOrder(rules, "") })
	if !strings.Contains(output, "IP 版本") || !strings.Contains(output, "IPv4+IPv6") || !strings.Contains(output, "2001:db8::/32") {
		t.Fatal("rule list omitted an IP version or source")
	}
	if err := runAccessRuleManager(inputUI(t, "5\no\n2\nt\ns\n\nq\ns\n\nq\n")); !errors.Is(err, ui.ErrReturnToMenu) {
		t.Fatal(err)
	}
	rules, err = rulespkg.ManagedRules(storage.ReadFileString(configPath))
	if err != nil || !reflect.DeepEqual(rules, []rulespkg.Rule{target, other}) {
		t.Fatalf("reordering split dual-stack traffic: %v %v", rules, err)
	}
	matches, err := accessPolicy().QueryRulesByPort(inputUI(t, "443\n"), rules)
	if err != nil || !reflect.DeepEqual(matches, []rulespkg.Rule{target}) {
		t.Fatalf("query split dual-stack rule: %v %v", matches, err)
	}
	if err := runAccessRuleManager(inputUI(t, "5\n"+target.ID+"\n2\n\nq\ns\n\nq\n")); !errors.Is(err, ui.ErrReturnToMenu) {
		t.Fatal(err)
	}
	remaining, err := rulespkg.ManagedRules(storage.ReadFileString(configPath))
	if err != nil || !reflect.DeepEqual(remaining, []rulespkg.Rule{other}) {
		t.Fatalf("deletion left part of the dual-stack rule or removed another rule: %v %v", remaining, err)
	}
}
