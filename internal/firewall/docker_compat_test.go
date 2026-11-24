package firewall

import (
	"strings"
	"testing"

	"snail_tool/internal/firewall/configuration/generate"
	"snail_tool/internal/firewall/forward/dockermark"
	rulespkg "snail_tool/internal/firewall/internal/rules"
)

func TestDockerMarkExistsOnlyWhileCompatIsOn(t *testing.T) {
	content, err := generate.Build([]int{22}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(content, dockermark.MarkExpression) {
		t.Fatal("generated forward rules are marked while Docker compat is off")
	}
	rule := rulespkg.Rule{ID: "aaaaaaaa", Kind: "source_port", Family: "ipv4", Source: "192.168.1.10", Ports: "443", Protocol: "tcp"}
	rule, err = rulespkg.NormalizeAllowRule(rule)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := rulespkg.ReplaceManagedRulesIn(content, []rulespkg.Rule{rule}, rulespkg.ForwardBlock)
	if err != nil || strings.Contains(plain, dockermark.MarkExpression) {
		t.Fatalf("unmarked forward edit gained a mark, err=%v", err)
	}
	markedBase, changed, err := dockermark.MarkForwardAccepts(content)
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	rules, err := rulespkg.ManagedRulesIn(markedBase, rulespkg.ForwardBlock)
	if err != nil || len(rules) != 0 {
		t.Fatalf("marked default config yielded custom rules: %+v, err=%v", rules, err)
	}
	marked, err := rulespkg.ReplaceManagedRulesIn(markedBase, []rulespkg.Rule{rule}, rulespkg.ForwardBlock)
	if err != nil || !strings.Contains(marked, dockermark.MarkExpression+" counter accept comment") {
		t.Fatalf("enabled forward edit lost the mark, err=%v", err)
	}

	withoutCounters, err := withPacketCounters(markedBase, false)
	if err != nil {
		t.Fatal(err)
	}
	marked, err = rulespkg.ReplaceManagedRulesIn(withoutCounters, []rulespkg.Rule{rule}, rulespkg.ForwardBlock)
	if err != nil || !strings.Contains(marked, dockermark.MarkExpression+" accept comment") {
		t.Fatalf("forward edit without counters lost the mark, err=%v", err)
	}
}
