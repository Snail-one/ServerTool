package firewall

import (
	"os"
	"strings"
	"testing"
)

func TestDockerSwitchesRefuseToOpenWithoutDocker(t *testing.T) {
	setupTest(t)
	if err := requireDockerPresent(); err == nil {
		t.Fatal("missing docker was accepted")
	}
	commandExists = func(name string) bool { return name == "docker" || name == "dockerd" }
	if err := requireDockerPresent(); err != nil {
		t.Fatal(err)
	}
	commandExists = func(name string) bool { return name == "dockerd" }
	if err := requireDockerPresent(); err != nil {
		t.Fatal(err)
	}
}

func TestDockerBridgeDirectionsAreIndependent(t *testing.T) {
	content, err := buildConfig([]int{22}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	off := dockerBridgeSettings{}
	if got, found, err := readDockerBridge(content); err != nil || !found || got != off {
		t.Fatalf("default settings = %v, found = %v, err = %v", got, found, err)
	}
	for _, tc := range []struct {
		name, match, other string
		settings           dockerBridgeSettings
	}{
		{"inbound", "oifname", "iifname", dockerBridgeSettings{inbound: true}},
		{"outbound", "iifname", "oifname", dockerBridgeSettings{outbound: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			updated, changed, err := withDockerBridge(content, tc.settings)
			if err != nil || !changed {
				t.Fatal(changed, err)
			}
			begin := strings.Index(updated, dockerBridgeBegin)
			end := strings.Index(updated, dockerBridgeEnd)
			if begin <= strings.Index(updated, "jump forward_source_allow") ||
				end >= strings.Index(updated, dockerBridgeNextRule) {
				t.Fatal("Docker rules must run after custom rules and before port lists")
			}
			for _, bridge := range []string{`"docker0"`, `"docker_gwbridge"`, `"br-*"`} {
				if !strings.Contains(updated[begin:end], tc.match+" "+bridge+" counter accept") ||
					strings.Contains(updated[begin:end], tc.other+" "+bridge) {
					t.Fatalf("incorrect interface match for %s: %s", bridge, updated[begin:end])
				}
			}
			if got, found, err := readDockerBridge(updated); err != nil || !found || got != tc.settings {
				t.Fatalf("saved settings = %v, found = %v, err = %v", got, found, err)
			}
			if again, changed, err := withDockerBridge(updated, tc.settings); err != nil || changed || again != updated {
				t.Fatal("identical settings should not rewrite the file", changed, err)
			}
			marked, _, err := withDockerForwardMark(updated)
			if err != nil || !strings.Contains(marked, "@snail_forward_public_ipv4_tcp_ports "+dockerForwardMarkExpr) {
				t.Fatal("compatibility marks were not applied to port-list rules", err)
			}
			if got, _, err := readDockerBridge(marked); err != nil || got != tc.settings {
				t.Fatal("compatibility marks changed the Docker bridge rules", got, err)
			}
			if closed, changed, err := withDockerBridge(updated, off); err != nil || !changed || closed != content {
				t.Fatal("disabling should remove the Docker bridge rules", changed, err)
			}
		})
	}
}

func TestDockerBridgeAcceptsLegacySettings(t *testing.T) {
	content, err := buildConfig([]int{22}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	legacy := strings.Replace(content, dockerBridgeBegin, dockerBridgeBegin+"\n        "+dockerBridgeLegacyJump, 1)
	both := dockerBridgeSettings{inbound: true, outbound: true}
	if got, found, err := readDockerBridge(legacy); err != nil || !found || got != both {
		t.Fatalf("legacy settings = %v, found = %v, err = %v", got, found, err)
	}
	outbound := dockerBridgeSettings{outbound: true}
	updated, changed, err := withDockerBridge(legacy, outbound)
	if err != nil || !changed || strings.Contains(updated, dockerBridgeLegacyJump) {
		t.Fatal("old switch was not converted", changed, err)
	}
	if got, _, err := readDockerBridge(updated); err != nil || got != outbound {
		t.Fatal("disabling legacy inbound also changed outbound", got, err)
	}
	withoutBlock := strings.Replace(content, "        "+dockerBridgeBegin+"\n        "+dockerBridgeEnd+"\n", "", 1)
	restored, changed, err := withDockerBridge(withoutBlock, outbound)
	if err != nil || !changed {
		t.Fatal("could not enable a direction on a pre-switch config", changed, err)
	}
	if got, found, err := readDockerBridge(restored); err != nil || !found || got != outbound {
		t.Fatal("incorrect settings on a pre-switch config", got, found, err)
	}
	damaged := strings.Replace(updated, `iifname "br-*" counter accept`, `iifname "br-*" counter drop`, 1)
	if _, changed, err := withDockerBridge(damaged, both); err == nil || changed {
		t.Fatal("overwrote a modified Docker bridge rule", changed, err)
	}
}

func TestDockerBridgeMenuAndRebuildPreserveOtherDirection(t *testing.T) {
	setupTest(t)
	version := ruleTestConfig(t, nil)
	content, err := withPacketCounters(string(version.content), false)
	if err != nil {
		t.Fatal(err)
	}
	content, _, err = withDockerBridge(content, dockerBridgeSettings{outbound: true})
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
	if err := configureDockerBridge(inputUI(t, "1\ny\n")); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	both := dockerBridgeSettings{inbound: true, outbound: true}
	if got, _, err := readDockerBridge(string(saved)); err != nil || got != both {
		t.Fatal("inbound toggle did not preserve outbound", got, err)
	}
	assertNoPacketCounters(t, string(saved))
	if err := configureDockerBridge(inputUI(t, "2\ny\n")); err != nil {
		t.Fatal(err)
	}
	if err := configure(inputUI(t, "\n\n\n\n1\n")); err != nil {
		t.Fatal(err)
	}
	saved, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, _, err := readDockerBridge(string(saved)); err != nil || got != (dockerBridgeSettings{inbound: true}) {
		t.Fatal("regeneration did not preserve the independent Docker bridge settings", got, err)
	}
	assertNoPacketCounters(t, string(saved))
}
