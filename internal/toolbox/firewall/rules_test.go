package firewall

import (
	"strings"
	"testing"
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
		got, err := normalizeSource(item.input)
		if err != nil || got != item.normalized || sourceRange(got) != item.covered {
			t.Fatalf("normalize %q: got=%q coverage=%q err=%v", item.input, got, sourceRange(got), err)
		}
	}
	for _, invalid := range []string{"", "192.168.1.1/33", "2001:db8::/129", "fe80::1%eth0", "1.2.3.4; accept", "$(reboot)", "192.168.999.1"} {
		if _, err := normalizeSource(invalid); err == nil {
			t.Fatalf("invalid source accepted: %q", invalid)
		}
	}
}

func TestRulePortsNormalizeRangesAndRejectInvalidInput(t *testing.T) {
	for _, item := range []struct{ input, normalized string }{
		{"443", "443"}, {"21069", "21069"}, {"8000-8100", "8000-8100"}, {"08000 - 08100", "8000-8100"}, {"443-443", "443"}, {"1-65535", "1-65535"},
	} {
		got, _, _, err := parseRulePorts(item.input)
		if err != nil || got != item.normalized {
			t.Fatalf("ports %q: got=%q err=%v", item.input, got, err)
		}
	}
	for _, invalid := range []string{"0", "-1", "+80", "80-+90", "65536", "8100-8000", "80-65536", "80,443", "80; accept", "", "80-90-100", "80\r", "80\u200b"} {
		if _, _, _, err := parseRulePorts(invalid); err == nil {
			t.Fatalf("invalid ports accepted: %q", invalid)
		}
	}
}

func TestAllRuleTypesAndProtocolsRenderOnlyRequestedIncomingTraffic(t *testing.T) {
	for _, kind := range []string{"port", "source", "source_port"} {
		for _, protocol := range []string{"tcp", "udp", "both"} {
			for _, source := range []string{"192.168.1.1/16", "2001:db8::/32"} {
				rule := allowRule{ID: "1234abcd", Kind: kind, Protocol: protocol}
				if kind != "port" {
					rule.Source = source
				}
				if kind != "source" {
					rule.Ports = "8000-8100"
				}
				rule, err := normalizeAllowRule(rule)
				if err != nil {
					t.Fatal(err)
				}
				lines := rule.nftLines()
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
	base, _ := normalizeAllowRule(allowRule{ID: "aaaaaaaa", Kind: "source_port", Source: "192.168.1.1/16", Ports: "443", Protocol: "tcp"})
	duplicate, _ := normalizeAllowRule(allowRule{ID: "bbbbbbbb", Kind: "source_port", Source: "192.168.2.2/16", Ports: "443-443", Protocol: "tcp"})
	if _, err := checkRuleRelations(duplicate, []allowRule{base}); err == nil {
		t.Fatal("equivalent CIDR/range must be rejected as a duplicate")
	}
	global := allowRule{ID: "cccccccc", Kind: "port", Ports: "443", Protocol: "tcp"}
	if warnings, err := checkRuleRelations(global, []allowRule{base}); err != nil || len(warnings) != 1 {
		t.Fatalf("global and source-specific rules must remain separate with a warning: %v %v", warnings, err)
	}
	if warnings, err := checkRuleRelations(base, []allowRule{global}); err != nil || len(warnings) != 1 {
		t.Fatalf("a covered source rule may be independently managed: %v %v", warnings, err)
	}
	udp := base
	udp.ID, udp.Protocol = "dddddddd", "udp"
	if warnings, err := checkRuleRelations(udp, []allowRule{base}); err != nil || len(warnings) != 0 {
		t.Fatal("TCP and UDP rules must not conflict")
	}
	ipv6 := base
	ipv6.ID, ipv6.Source = "eeeeeeee", "2001:db8::/32"
	if warnings, err := checkRuleRelations(ipv6, []allowRule{base}); err != nil || len(warnings) != 0 {
		t.Fatal("IPv4 and IPv6 source restrictions must not conflict")
	}
}
