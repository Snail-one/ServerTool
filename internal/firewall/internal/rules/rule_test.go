package rules_test

import (
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/firewall/configuration/generate"
	"snail_tool/internal/firewall/forward/dockermark"
	rulespkg "snail_tool/internal/firewall/internal/rules"
)

func TestNormalizeRuleSourcesAndActualCoverage(t *testing.T) {
	for _, item := range []struct{ input, normalized, covered string }{
		{"192.168.1.1", "192.168.1.1", "192.168.1.1"},
		{"192.168.1.0/24", "192.168.1.0/24", "192.168.1.0 至 192.168.1.255"},
		{"192.168.1.1/16", "192.168.0.0/16", "192.168.0.0 至 192.168.255.255"},
		{"10.0.0.0/8", "10.0.0.0/8", "10.0.0.0 至 10.255.255.255"},
		{"172.16.0.0/12", "172.16.0.0/12", "172.16.0.0 至 172.31.255.255"},
		{"2001:db8:abcd::1/32", "2001:db8::/32", "2001:db8:: 至 2001:db8:ffff:ffff:ffff:ffff:ffff:ffff"},
		{"2001:db8::1/128", "2001:db8::1", "2001:db8::1"},
		{"192.168.1.1/32", "192.168.1.1", "192.168.1.1"},
		{"::ffff:192.168.1.1/120", "192.168.1.0/24", "192.168.1.0 至 192.168.1.255"},
	} {
		got, err := configuration.NormalizeSource(item.input)
		if err != nil || got != item.normalized || rulespkg.SourceRange(got) != item.covered {
			t.Fatalf("normalize %q: got=%q coverage=%q err=%v", item.input, got, rulespkg.SourceRange(got), err)
		}
	}
	for _, invalid := range []string{"", "192.168.1.1/33", "2001:db8::/129", "fe80::1%eth0", "1.2.3.4; accept", "$(reboot)", "192.168.999.1"} {
		if _, err := configuration.NormalizeSource(invalid); err == nil {
			t.Fatalf("invalid source accepted: %q", invalid)
		}
	}
}

func TestRulePortsNormalizeRangesAndRejectInvalidInput(t *testing.T) {
	for _, item := range []struct{ input, normalized string }{
		{"443", "443"}, {"21069", "21069"}, {"8000-8100", "8000-8100"}, {"08000 - 08100", "8000-8100"}, {"443-443", "443"}, {"1-65535", "1-65535"},
	} {
		got, _, _, err := configuration.ParsePortRange(item.input)
		if err != nil || got != item.normalized {
			t.Fatalf("ports %q: got=%q err=%v", item.input, got, err)
		}
	}
	for _, invalid := range []string{"0", "-1", "+80", "80-+90", "65536", "8100-8000", "80-65536", "80,443", "80; accept", "", "80-90-100", "80\r", "80\u200b"} {
		if _, _, _, err := configuration.ParsePortRange(invalid); err == nil {
			t.Fatalf("invalid ports accepted: %q", invalid)
		}
	}
}

func TestAllRuleTypesAndProtocolsRenderOnlyRequestedIncomingTraffic(t *testing.T) {
	for _, kind := range []string{"port", "source", "source_port"} {
		for _, protocol := range []string{"tcp", "udp", "both"} {
			for _, source := range []string{"192.168.1.1/16", "2001:db8::/32"} {
				rule := rulespkg.Rule{ID: "1234abcd", Kind: kind, Protocol: protocol}
				if kind != "port" {
					rule.Source = source
				}
				if kind != "source" {
					rule.Ports = "8000-8100"
				}
				rule, err := rulespkg.NormalizeAllowRule(rule)
				if err != nil {
					t.Fatal(err)
				}
				lines := rule.NftLines()
				want := 1
				if protocol == "both" {
					want = 2
				}
				if len(lines) != want {
					t.Fatalf("%s/%s emitted %d rules, want %d", kind, protocol, len(lines), want)
				}
				for _, line := range lines {
					if strings.Contains(line, "daddr") || !strings.Contains(line, `comment "snail-rule:1234abcd"`) || !strings.Contains(line, "counter accept") {
						t.Fatalf("rule must match incoming sources and keep its ID: %s", line)
					}
					if kind == "source" && (strings.Contains(line, "dport") || !strings.Contains(line, "meta l4proto ")) {
						t.Fatalf("IP allow must cover all ports of only selected protocols: %s", line)
					}
					if kind != "source" && !strings.Contains(line, "dport 8000-8100") {
						t.Fatalf("port range missing: %s", line)
					}
					if kind != "port" {
						family := "ip6"
						if strings.HasPrefix(source, "192") {
							family = "ip"
						}
						if !strings.HasPrefix(line, family+" saddr "+rule.Source+" ") {
							t.Fatalf("wrong address family or direction: %s", line)
						}
					}
				}
			}
		}
	}
}

func TestNormalizedDuplicatesAreRejectedButOverlappingRulesStayIndependent(t *testing.T) {
	base, _ := rulespkg.NormalizeAllowRule(rulespkg.Rule{ID: "aaaaaaaa", Kind: "source_port", Source: "192.168.1.1/16", Ports: "443", Protocol: "tcp"})
	duplicate, _ := rulespkg.NormalizeAllowRule(rulespkg.Rule{ID: "bbbbbbbb", Kind: "source_port", Source: "192.168.2.2/16", Ports: "443-443", Protocol: "tcp"})
	if _, err := rulespkg.CheckRuleRelations(duplicate, []rulespkg.Rule{base}); err == nil {
		t.Fatal("equivalent CIDR/range must be rejected as a duplicate")
	}
	global := rulespkg.Rule{ID: "cccccccc", Kind: "port", Ports: "443", Protocol: "tcp"}
	if warnings, err := rulespkg.CheckRuleRelations(global, []rulespkg.Rule{base}); err != nil || len(warnings) != 1 {
		t.Fatalf("global and source-specific rules must remain separate with a warning: %v %v", warnings, err)
	}
	if warnings, err := rulespkg.CheckRuleRelations(base, []rulespkg.Rule{global}); err != nil || len(warnings) != 1 {
		t.Fatalf("a covered source rule may be independently managed: %v %v", warnings, err)
	}
	udp := base
	udp.ID, udp.Protocol = "dddddddd", "udp"
	if warnings, err := rulespkg.CheckRuleRelations(udp, []rulespkg.Rule{base}); err != nil || len(warnings) != 0 {
		t.Fatal("TCP and UDP rules must not conflict")
	}
	ipv6 := base
	ipv6.ID, ipv6.Source = "eeeeeeee", "2001:db8::/32"
	if warnings, err := rulespkg.CheckRuleRelations(ipv6, []rulespkg.Rule{base}); err != nil || len(warnings) != 0 {
		t.Fatal("IPv4 and IPv6 source restrictions must not conflict")
	}
}

func TestFullRuleDropsAtTheFrontAndLegacyLinesStayUnchanged(t *testing.T) {
	legacy, err := rulespkg.NormalizeAllowRule(rulespkg.Rule{ID: "aaaaaaaa", Kind: "source_port", Source: "192.168.1.10", Ports: "443", Protocol: "tcp"})
	if err != nil {
		t.Fatal(err)
	}
	legacyLine := strings.Join(legacy.NftLines(), "\n")
	if legacy.Kind != "source_port" || strings.Contains(legacyLine, "daddr") || strings.Contains(legacyLine, "counter drop") || legacyLine != `ip saddr 192.168.1.10 tcp dport 443 counter accept comment "snail-rule:aaaaaaaa"` {
		t.Fatalf("legacy source rule changed: %s", legacyLine)
	}
	portOnly, err := rulespkg.NormalizeAllowRule(rulespkg.Rule{ID: "bbbbbbbb", Family: "ipv4", Ports: "443", Protocol: "tcp"})
	if err != nil || portOnly.Kind != "rule" {
		t.Fatalf("port-only accept must be a new rule, not a legacy port exception: %+v %v", portOnly, err)
	}
	kept, err := rulespkg.NormalizeAllowRule(rulespkg.Rule{ID: "cccccccc", Kind: "port", Family: "ipv4", Ports: "443", Protocol: "tcp"})
	if err != nil || kept.Kind != "port" || strings.Contains(strings.Join(kept.NftLines(), "\n"), "daddr") {
		t.Fatalf("legacy port exception was rewritten: %+v %v", kept, err)
	}
	drop, err := rulespkg.NormalizeAllowRule(rulespkg.Rule{
		ID: "dddddddd", Family: "ipv4", Verdict: "drop", Source: "203.0.113.10", Dest: "192.0.2.20",
		Iif: "eth0", Oif: "br-*", Sport: "40000-40010", Ports: "3000", Protocol: "tcp",
	})
	if err != nil || drop.Kind != "rule" {
		t.Fatalf("drop rule was not accepted: %+v %v", drop, err)
	}
	wantDrop := `iifname "eth0" oifname "br-*" ip saddr 203.0.113.10 ip daddr 192.0.2.20 tcp sport 40000-40010 tcp dport 3000 counter drop comment "snail-rule:dddddddd"`
	if got := strings.Join(drop.NftLines(), "\n"); got != wantDrop || strings.Contains(got, dockermark.MarkExpression) {
		t.Fatalf("drop line = %s", got)
	}
	v6, err := rulespkg.NormalizeAllowRule(rulespkg.Rule{
		ID: "eeeeeeee", Family: "ipv6", Verdict: "drop", Source: "2001:db8::5", Dest: "2001:db8::9", Iif: "eth0", Ports: "3000", Protocol: "tcp",
	})
	if err != nil {
		t.Fatal(err)
	}
	v6Line := strings.Join(v6.NftLines(), "\n")
	if v6Line != `iifname "eth0" ip6 saddr 2001:db8::5 ip6 daddr 2001:db8::9 tcp dport 3000 counter drop comment "snail-rule:eeeeeeee"` {
		t.Fatalf("IPv6 drop used the wrong family: %s", v6Line)
	}
	if _, err := rulespkg.NormalizeAllowRule(rulespkg.Rule{ID: "ffffffff", Protocol: "tcp"}); err == nil {
		t.Fatal("a rule with no match was accepted")
	}
	base, err := generate.Build([]int{2222}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	content, err := rulespkg.ReplaceManagedRules(base, []rulespkg.Rule{drop, legacy})
	if err != nil {
		t.Fatal(err)
	}
	got, err := rulespkg.ManagedRules(content)
	if err != nil || !reflect.DeepEqual(got, []rulespkg.Rule{drop, legacy}) {
		t.Fatalf("full rule did not round-trip: %v %v", got, err)
	}
	markedBase, changed, err := dockermark.MarkForwardAccepts(base)
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	marked, err := rulespkg.ReplaceManagedRulesIn(markedBase, []rulespkg.Rule{drop, legacy}, rulespkg.ForwardBlock)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(marked, "\n") {
		if strings.Contains(line, "counter drop") && strings.Contains(line, dockermark.MarkExpression) {
			t.Fatalf("drop was marked: %s", line)
		}
		if strings.Contains(line, "snail-rule:aaaaaaaa") && !strings.Contains(line, dockermark.MarkExpression+" counter accept") {
			t.Fatalf("forward accept lost its mark: %s", line)
		}
	}
}
