package rules

import "testing"

func TestRuleRejectsMismatchedOrMissingFamilySources(t *testing.T) {
	for _, rule := range []Rule{
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
		if _, err := NormalizeAllowRule(rule); err == nil {
			t.Fatalf("invalid family/source accepted: %+v", rule)
		}
	}
}

func TestRuleRelationsRespectIndependentIPVersions(t *testing.T) {
	v4 := Rule{ID: "aaaaaaaa", Kind: "port", Family: "ipv4", Ports: "443", Protocol: "tcp"}
	v6 := v4
	v6.ID, v6.Family = "bbbbbbbb", "ipv6"
	both := v4
	both.ID, both.Family = "cccccccc", "both"
	if warnings, err := CheckRuleRelations(v6, []Rule{v4}); err != nil || len(warnings) != 0 {
		t.Fatalf("IPv4 and IPv6 must be independently allowed: %v %v", warnings, err)
	}
	if v4.Overlaps(v6) || v4.Covers(v6) || v4.Covers(both) || !both.Covers(v4) || !both.Covers(v6) {
		t.Fatal("incorrect IP version coverage")
	}
	legacy := both
	legacy.ID, legacy.Family = "dddddddd", ""
	if _, err := CheckRuleRelations(legacy, []Rule{both}); err == nil {
		t.Fatal("legacy dual-stack and explicit dual-stack duplicates accepted")
	}
	sources := Rule{ID: "eeeeeeee", Kind: "source_port", Family: "both", Source: "192.168.0.0/16", SourceIPv6: "2001:db8::/32", Ports: "443", Protocol: "tcp"}
	inside := Rule{ID: "ffffffff", Kind: "source_port", Family: "ipv6", Source: "2001:db8:1::/48", Ports: "443", Protocol: "tcp"}
	if !sources.Covers(inside) || !sources.Overlaps(inside) || inside.Covers(sources) {
		t.Fatal("dual-stack source coverage ignored one address family")
	}
	inside.Source = "2001:db9::/32"
	if sources.Overlaps(inside) {
		t.Fatal("unrelated IPv6 prefixes overlap")
	}
}
