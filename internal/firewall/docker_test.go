package firewall

import (
	"os"
	"strings"
	"testing"

	"snail_tool/internal/firewall/configuration/generate"
	"snail_tool/internal/firewall/forward/dockerbridge"
	"snail_tool/internal/firewall/forward/dockermark"
	rulespkg "snail_tool/internal/firewall/internal/rules"
)

func TestDockerSwitchesRefuseToOpenWithoutDocker(t *testing.T) {
	setupTest(t)
	if err := dockerMarkService().RequireDocker(); err == nil {
		t.Fatal("missing docker was accepted")
	}
	commandExists = func(name string) bool { return name == "docker" || name == "dockerd" }
	if err := dockerMarkService().RequireDocker(); err != nil {
		t.Fatal(err)
	}
	commandExists = func(name string) bool { return name == "dockerd" }
	if err := dockerMarkService().RequireDocker(); err != nil {
		t.Fatal(err)
	}
}

func TestDockerBridgeDirectionsAreIndependent(t *testing.T) {
	content, err := generate.Build([]int{22}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	off := dockerbridge.State{}
	if got, found, err := dockerbridge.Read(content); err != nil || !found || got != off {
		t.Fatalf("default settings = %v, found = %v, err = %v", got, found, err)
	}
	for _, tc := range []struct {
		name, match, other string
		settings           dockerbridge.State
	}{
		{"inbound", "oifname", "iifname", dockerbridge.State{Inbound: true}},
		{"outbound", "iifname", "oifname", dockerbridge.State{Outbound: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			updated, changed, err := dockerbridge.Replace(content, tc.settings)
			if err != nil || !changed {
				t.Fatal(changed, err)
			}
			begin := strings.Index(updated, dockerbridge.Begin)
			end := strings.Index(updated, dockerbridge.End)
			if begin <= strings.Index(updated, "jump forward_source_allow") ||
				end >= strings.Index(updated, "ip saddr $snail_forward_private_ipv4 goto forward_private_ports") {
				t.Fatal("Docker rules must run after custom rules and before port lists")
			}
			for _, bridge := range []string{`"docker0"`, `"docker_gwbridge"`, `"br-*"`} {
				if !strings.Contains(updated[begin:end], tc.match+" "+bridge+" counter accept") ||
					strings.Contains(updated[begin:end], tc.other+" "+bridge) {
					t.Fatalf("incorrect interface match for %s: %s", bridge, updated[begin:end])
				}
			}
			if got, found, err := dockerbridge.Read(updated); err != nil || !found || got != tc.settings {
				t.Fatalf("saved settings = %v, found = %v, err = %v", got, found, err)
			}
			if again, changed, err := dockerbridge.Replace(updated, tc.settings); err != nil || changed || again != updated {
				t.Fatal("identical settings should not rewrite the file", changed, err)
			}
			marked, _, err := dockermark.MarkForwardAccepts(updated)
			if err != nil || !strings.Contains(marked, "@snail_forward_public_ipv4_tcp_ports "+dockermark.MarkExpression) {
				t.Fatal("compatibility marks were not applied to port-list rules", err)
			}
			if got, _, err := dockerbridge.Read(marked); err != nil || got != tc.settings {
				t.Fatal("compatibility marks changed the Docker bridge rules", got, err)
			}
			if closed, changed, err := dockerbridge.Replace(updated, off); err != nil || !changed || closed != content {
				t.Fatal("disabling should remove the Docker bridge rules", changed, err)
			}
		})
	}
}

func TestDockerBridgeAcceptsLegacySettings(t *testing.T) {
	content, err := generate.Build([]int{22}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	legacy := strings.Replace(content, dockerbridge.Begin, dockerbridge.Begin+"\n        "+"jump forward_docker", 1)
	both := dockerbridge.State{Inbound: true, Outbound: true}
	if got, found, err := dockerbridge.Read(legacy); err != nil || !found || got != both {
		t.Fatalf("legacy settings = %v, found = %v, err = %v", got, found, err)
	}
	outbound := dockerbridge.State{Outbound: true}
	updated, changed, err := dockerbridge.Replace(legacy, outbound)
	if err != nil || !changed || strings.Contains(updated, "jump forward_docker") {
		t.Fatal("old switch was not converted", changed, err)
	}
	if got, _, err := dockerbridge.Read(updated); err != nil || got != outbound {
		t.Fatal("disabling legacy inbound also changed outbound", got, err)
	}
	withoutBlock := strings.Replace(content, "        "+dockerbridge.Begin+"\n        "+dockerbridge.End+"\n", "", 1)
	restored, changed, err := dockerbridge.Replace(withoutBlock, outbound)
	if err != nil || !changed {
		t.Fatal("could not enable a direction on a pre-switch config", changed, err)
	}
	if got, found, err := dockerbridge.Read(restored); err != nil || !found || got != outbound {
		t.Fatal("incorrect settings on a pre-switch config", got, found, err)
	}
	damaged := strings.Replace(updated, `iifname "br-*" counter accept`, `iifname "br-*" counter drop`, 1)
	if _, changed, err := dockerbridge.Replace(damaged, both); err == nil || changed {
		t.Fatal("overwrote a modified Docker bridge rule", changed, err)
	}
}

func TestDockerBridgeMenuAndRebuildPreserveOtherDirection(t *testing.T) {
	setupTest(t)
	version := ruleTestConfig(t, nil)
	content, err := withPacketCounters(string(version.Content), false)
	if err != nil {
		t.Fatal(err)
	}
	content, _, err = dockerbridge.Replace(content, dockerbridge.State{Outbound: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	acceptRuleCommands(t)
	commandExists = func(name string) bool { return name == "nft" || name == "docker" }
	accept := commandOutput
	commandOutput = func(name string, args ...string) (string, error) {
		if isSSHDTest(name, args) {
			return sshdTestConfig(2222), nil
		}
		return accept(name, args...)
	}
	if err := dockerBridgeService().Run(inputUI(t, "1\ny\n")); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	both := dockerbridge.State{Inbound: true, Outbound: true}
	if got, _, err := dockerbridge.Read(string(saved)); err != nil || got != both {
		t.Fatal("inbound toggle did not preserve outbound", got, err)
	}
	assertNoPacketCounters(t, string(saved))
	if err := dockerBridgeService().Run(inputUI(t, "2\ny\n")); err != nil {
		t.Fatal(err)
	}
	if err := configure(inputUI(t, "\n\n\n\n1\n")); err != nil {
		t.Fatal(err)
	}
	saved, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, _, err := dockerbridge.Read(string(saved)); err != nil || got != (dockerbridge.State{Inbound: true}) {
		t.Fatal("regeneration did not preserve the independent Docker bridge settings", got, err)
	}
	assertNoPacketCounters(t, string(saved))
}

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
