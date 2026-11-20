package firewall

import (
	"strings"
	"testing"
)

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
	if err != nil || strings.Contains(marked, `iifname "docker0" ct mark`) || !strings.Contains(marked, "ct state established,related ct mark set ct mark or 0x00800000 counter accept") {
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
}
