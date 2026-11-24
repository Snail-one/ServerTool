package firewall

import (
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

func TestDockerBridgeStaysOffUntilEnabled(t *testing.T) {
	content, err := buildConfig([]int{22}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "chain forward_docker {") || !strings.Contains(content, `iifname "br-*"`) {
		t.Fatal("docker bridge chain is missing")
	}
	enabled, found, err := readDockerBridge(content)
	if err != nil || enabled || !found || strings.Contains(content, dockerBridgeJump) {
		t.Fatalf("default bridge switch = enabled:%v found:%v err:%v", enabled, found, err)
	}
	opened, changed, err := withDockerBridge(content, true)
	if err != nil || !changed || !strings.Contains(opened, "jump forward_source_allow\n        "+dockerBridgeBegin+"\n        "+dockerBridgeJump) {
		t.Fatal(changed, err)
	}
	if strings.Contains(opened, `iifname "docker0" ct mark`) {
		t.Fatal("docker bridge accept was marked")
	}
	again, changed, err := withDockerBridge(opened, true)
	if err != nil || changed || again != opened {
		t.Fatal("enabling twice changed the config", changed, err)
	}
	closed, changed, err := withDockerBridge(opened, false)
	if err != nil || !changed || strings.Contains(closed, dockerBridgeJump) {
		t.Fatal(changed, err)
	}
	marked, _, err := withDockerForwardMark(opened)
	if err != nil || strings.Contains(marked, `iifname "docker0" ct mark`) ||
		strings.Contains(marked, "ct state established,related "+dockerForwardMarkExpr) ||
		!strings.Contains(marked, "@snail_forward_public_ipv4_tcp_ports "+dockerForwardMarkExpr) {
		t.Fatal("compat mark reached the docker bridge chain", err)
	}
	legacy := strings.Replace(content, "        "+dockerBridgeBegin+"\n        "+dockerBridgeEnd+"\n", "", 1)
	if strings.Contains(legacy, dockerBridgeBegin) {
		t.Fatal("failed to build a legacy config")
	}
	restored, changed, err := withDockerBridge(legacy, true)
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	if enabled, found, err := readDockerBridge(restored); err != nil || !enabled || !found {
		t.Fatalf("legacy enable = %v %v %v", enabled, found, err)
	}
	rebuilt, err := buildConfig([]int{22}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	kept, _, err := withDockerBridge(rebuilt, true)
	if err != nil || !strings.Contains(kept, dockerBridgeJump) {
		t.Fatal(err)
	}
	if _, _, err := readDockerBridge(strings.Replace(opened, dockerBridgeEnd, dockerBridgeEnd+"\n"+dockerBridgeEnd, 1)); err == nil {
		t.Fatal("damaged docker bridge marker was accepted")
	}
	withoutChain := content
	chainStart := strings.Index(withoutChain, "    chain forward_docker {")
	chainEnd := strings.Index(withoutChain, "    chain forward {")
	if chainStart < 0 || chainEnd < chainStart {
		t.Fatal("template chain layout changed")
	}
	withoutChain = withoutChain[:chainStart] + withoutChain[chainEnd:]
	withoutChain = strings.Replace(withoutChain, "        "+dockerBridgeBegin+"\n        "+dockerBridgeEnd+"\n", "", 1)
	if strings.Contains(withoutChain, "chain forward_docker {") {
		t.Fatal("old config still has the docker chain")
	}
	repaired, changed, err := withDockerBridge(withoutChain, true)
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	chainAt, jumpAt := strings.Index(repaired, "chain forward_docker {"), strings.Index(repaired, dockerBridgeJump)
	if chainAt < 0 || jumpAt < chainAt || !strings.Contains(repaired, `iifname "br-*"`) {
		t.Fatal("enabling an old config did not add the chain before the jump")
	}
}
