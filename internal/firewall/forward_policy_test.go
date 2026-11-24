package firewall

import (
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/firewall/access"
	"snail_tool/internal/firewall/configuration/generate"
	"snail_tool/internal/firewall/forward"
	rulespkg "snail_tool/internal/firewall/internal/rules"
)

func TestForwardPortsStayEmptyAndSeparate(t *testing.T) {
	content, err := generate.Build([]int{22}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"type filter hook forward priority -10; policy drop;",
		"jump forward_source_allow",
		"ip saddr $snail_forward_private_ipv4 goto forward_private_ports",
		"ip6 saddr $snail_forward_private_ipv6 goto forward_private_ports",
		"goto forward_public_ports",
		"# ===== BEGIN SNAIL FORWARD RULES =====",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("missing forward rule %q", required)
		}
	}
	for _, list := range forward.Lists[:8] {
		value, err := rulespkg.ReadPolicyValue(content, list)
		if err != nil || value != "" {
			t.Fatalf("forward port list %s = %q, err=%v", list.Name, value, err)
		}
	}
	accessPorts, err := rulespkg.ReadPolicyValue(content, access.Lists[0])
	if err != nil || accessPorts != "22, 80, 443" {
		t.Fatalf("access public ports changed or missing web ports: %q %v", accessPorts, err)
	}
	updated, err := rulespkg.WithPolicyValue(content, forward.Lists[0], "8443, 8000-8100")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := rulespkg.ReadPolicyValue(updated, forward.Lists[0]); err != nil || got != "8000-8100, 8443" {
		t.Fatalf("forward port edit = %q, err=%v", got, err)
	}
	if got, err := rulespkg.ReadPolicyValue(updated, access.Lists[0]); err != nil || got != "22, 80, 443" {
		t.Fatalf("forward edit changed access ports: %q %v", got, err)
	}
	if strings.Contains(updated, "$snail_ssh_port") && strings.Contains(updated[strings.Index(updated, "set snail_forward_public_ipv4_tcp_ports"):], "$snail_ssh_port") {
		t.Fatal("forward port set must not keep the SSH port reference")
	}
	cleared, err := rulespkg.WithPolicyValue(updated, forward.Lists[0], "")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := rulespkg.ReadPolicyValue(cleared, forward.Lists[0]); err != nil || got != "" {
		t.Fatalf("cleared forward ports = %q, err=%v", got, err)
	}
	if strings.Contains(cleared, "elements =") && strings.Contains(cleared[strings.Index(cleared, "set snail_forward_public_ipv4_tcp_ports"):strings.Index(cleared, "set snail_forward_public_ipv4_udp_ports")], "elements") {
		t.Fatal("cleared forward port set must not keep an elements line")
	}
}

func TestForwardSourceRulesDoNotChangeAccessRules(t *testing.T) {
	content, err := generate.Build([]int{22}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	rule := rulespkg.Rule{ID: "aaaaaaaa", Kind: "source_port", Family: "ipv4", Source: "192.168.1.10", Ports: "443", Protocol: "tcp"}
	rule, err = rulespkg.NormalizeAllowRule(rule)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := rulespkg.ReplaceManagedRulesIn(content, []rulespkg.Rule{rule}, rulespkg.ForwardBlock)
	if err != nil {
		t.Fatal(err)
	}
	got, err := rulespkg.ManagedRulesIn(updated, rulespkg.ForwardBlock)
	if err != nil || !reflect.DeepEqual(got, []rulespkg.Rule{rule}) {
		t.Fatalf("forward rules = %+v, err=%v", got, err)
	}
	accessPorts, err := rulespkg.ManagedRulesIn(updated, rulespkg.AccessBlock)
	if err != nil || len(accessPorts) != 0 {
		t.Fatalf("access rules changed: %+v %v", accessPorts, err)
	}
	if _, err := rulespkg.ChangePolicyValue(forward.Lists[8], "172.16.0.0/12, 192.168.0.0/16", "reset", "", -1); err != nil {
		t.Fatal(err)
	}
	reset, err := rulespkg.ChangePolicyValue(forward.Lists[8], "10.1.0.0/16", "reset", "", -1)
	if err != nil || reset != forward.Lists[8].Defaults {
		t.Fatalf("forward range reset = %q, err=%v", reset, err)
	}
}
