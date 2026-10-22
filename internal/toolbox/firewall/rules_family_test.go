package firewall

import (
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/shared"
)

func TestRuleFamiliesRenderAndRoundTripWithoutBroadeningTraffic(t *testing.T) {
	for _, kind := range []string{"port", "source", "source_port"} {
		for _, family := range []string{"ipv4", "ipv6", "both"} {
			t.Run(kind+"/"+family, func(t *testing.T) {
				rule := allowRule{ID: "1234abcd", Kind: kind, Family: family, Protocol: "both"}
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
				rule, err := normalizeAllowRule(rule)
				if err != nil {
					t.Fatal(err)
				}
				lines := rule.nftLines()
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
				base, _ := buildConfig([]int{2222}, false, false)
				content, err := replaceManagedRules(base, []allowRule{rule})
				if err != nil {
					t.Fatal(err)
				}
				got, err := managedRules(content)
				if err != nil || !reflect.DeepEqual(got, []allowRule{rule}) {
					t.Fatalf("IP version or source lost on reload: %v %v", got, err)
				}
			})
		}
	}
}

func TestRuleRejectsMismatchedOrMissingFamilySources(t *testing.T) {
	for _, rule := range []allowRule{
		{Family: "ipv4", Source: "2001:db8::/32"},
		{Family: "ipv6", Source: "192.168.1.1"},
		{Family: "both", Source: "192.168.1.1"},
		{Family: "both", Source: "2001:db8::1", SourceIPv6: "2001:db8::2"},
		{Family: "both", Source: "192.168.1.1", SourceIPv6: "192.168.1.2"},
		{Family: "ipv4", Source: "192.168.1.1", SourceIPv6: "2001:db8::1"},
		{Source: "192.168.1.1", SourceIPv6: "2001:db8::1"},
		{Family: "invalid", Source: "192.168.1.1"},
	} {
		rule.ID, rule.Kind, rule.Protocol, rule.Ports = "1234abcd", "source_port", "tcp", "443"
		if _, err := normalizeAllowRule(rule); err == nil {
			t.Fatalf("invalid family/source accepted: %+v", rule)
		}
	}
}

func TestRuleRelationsRespectIndependentIPVersions(t *testing.T) {
	v4 := allowRule{ID: "aaaaaaaa", Kind: "port", Family: "ipv4", Ports: "443", Protocol: "tcp"}
	v6 := v4
	v6.ID, v6.Family = "bbbbbbbb", "ipv6"
	both := v4
	both.ID, both.Family = "cccccccc", "both"
	if warnings, err := checkRuleRelations(v6, []allowRule{v4}); err != nil || len(warnings) != 0 {
		t.Fatalf("IPv4 and IPv6 must be independently allowed: %v %v", warnings, err)
	}
	if v4.overlaps(v6) || v4.covers(v6) || v4.covers(both) || !both.covers(v4) || !both.covers(v6) {
		t.Fatal("incorrect IP version coverage")
	}
	legacy := both
	legacy.ID, legacy.Family = "dddddddd", ""
	if _, err := checkRuleRelations(legacy, []allowRule{both}); err == nil {
		t.Fatal("legacy dual-stack and explicit dual-stack duplicates accepted")
	}
	sources := allowRule{ID: "eeeeeeee", Kind: "source_port", Family: "both", Source: "192.168.0.0/16", SourceIPv6: "2001:db8::/32", Ports: "443", Protocol: "tcp"}
	inside := allowRule{ID: "ffffffff", Kind: "source_port", Family: "ipv6", Source: "2001:db8:1::/48", Ports: "443", Protocol: "tcp"}
	if !sources.covers(inside) || !sources.overlaps(inside) || inside.covers(sources) {
		t.Fatal("dual-stack source coverage ignored one address family")
	}
	inside.Source = "2001:db9::/32"
	if sources.overlaps(inside) {
		t.Fatal("unrelated IPv6 prefixes overlap")
	}
}

func TestAskRuleSupportsSeparateAndCombinedIPVersions(t *testing.T) {
	for _, test := range []struct{ input, family, source, source6 string }{
		{"1\n1\n443\n1\n", "ipv4", "", ""},
		{"1\n2\n443\n1\n", "ipv6", "", ""},
		{"1\n3\n443\n1\n", "both", "", ""},
		{"3\n2\n192.168.1.1\n2001:db8::1\n443\n1\n", "ipv6", "2001:db8::1", ""},
		{"3\n3\n192.168.1.1/16\n2001:db8::1/32\n443\n1\n", "both", "192.168.0.0/16", "2001:db8::/32"},
	} {
		rule, err := askAllowRule(inputUI(t, test.input), allowRule{ID: "1234abcd"})
		if err != nil || rule.Family != test.family || rule.Source != test.source || rule.SourceIPv6 != test.source6 {
			t.Fatalf("wrong family/source after input: %+v %v", rule, err)
		}
	}
}

func TestEditDualStackSourcesToOneVersionKeepsTheChosenAddress(t *testing.T) {
	current := allowRule{ID: "1234abcd", Kind: "source_port", Family: "both", Source: "192.168.0.0/16", SourceIPv6: "2001:db8::/32", Ports: "443", Protocol: "tcp"}
	rule, err := askAllowRule(inputUI(t, "\n2\n\n\n\n"), current)
	if err != nil || rule.Family != "ipv6" || rule.Source != current.SourceIPv6 || rule.SourceIPv6 != "" {
		t.Fatalf("switching to IPv6 kept the wrong source: %+v %v", rule, err)
	}
}

func TestDualStackSourceRuleIsSavedMovedQueriedAndDeletedAsOneID(t *testing.T) {
	setupTest(t)
	other := allowRule{ID: "aaaaaaaa", Kind: "port", Family: "ipv4", Ports: "53", Protocol: "udp"}
	version := ruleTestConfig(t, []allowRule{other})
	acceptRuleCommands(t)
	if err := changeAllowRule(inputUI(t, "3\n3\n192.168.1.1/16\n2001:db8::1/32\n443\n3\ny\n"), version, []allowRule{other}, ""); err != nil {
		t.Fatal(err)
	}
	rules, err := managedRules(shared.ReadFileString(configPath))
	if err != nil || len(rules) != 2 || rules[1].Family != "both" || len(rules[1].nftLines()) != 4 {
		t.Fatalf("dual-stack sources were not stored as one logical rule: %v %v", rules, err)
	}
	target := rules[1]
	output := capturePortOutput(t, func() { printRuleOrder(rules, "") })
	if !strings.Contains(output, "IP 版本") || !strings.Contains(output, "IPv4+IPv6") || !strings.Contains(output, "2001:db8::/32") {
		t.Fatal("rule list omitted an IP version or source")
	}
	version, err = readConfigVersion()
	if err != nil {
		t.Fatal(err)
	}
	if err := reorderAllowRules(inputUI(t, "2\nt\ns\ny\n"), version, rules); err != nil {
		t.Fatal(err)
	}
	rules, err = managedRules(shared.ReadFileString(configPath))
	if err != nil || !reflect.DeepEqual(rules, []allowRule{target, other}) {
		t.Fatalf("reordering split dual-stack traffic: %v %v", rules, err)
	}
	matches, err := queryRulesByPort(inputUI(t, "443\n"), rules)
	if err != nil || !reflect.DeepEqual(matches, []allowRule{target}) {
		t.Fatalf("query split dual-stack rule: %v %v", matches, err)
	}
	version, err = readConfigVersion()
	if err != nil {
		t.Fatal(err)
	}
	if err := deleteAllowRule(inputUI(t, "1\n"+target.ID+"\ny\n"), version, rules); err != nil {
		t.Fatal(err)
	}
	remaining, err := managedRules(shared.ReadFileString(configPath))
	if err != nil || !reflect.DeepEqual(remaining, []allowRule{other}) {
		t.Fatalf("deletion left part of the dual-stack rule or removed another rule: %v %v", remaining, err)
	}
}
