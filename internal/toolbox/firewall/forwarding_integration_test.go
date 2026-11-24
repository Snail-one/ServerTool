//go:build linux && integration

package firewall

import (
	"context"
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

//go:embed testdata/forwarding_integration.sh
var forwardingIntegrationScript []byte

func TestNftablesForwardingIntegration(t *testing.T) {
	base, err := buildConfig([]int{22}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	inbound, _, err := withDockerBridge(base, dockerBridgeSettings{inbound: true})
	if err != nil {
		t.Fatal(err)
	}
	outbound, _, err := withDockerBridge(base, dockerBridgeSettings{outbound: true})
	if err != nil {
		t.Fatal(err)
	}
	ports, err := withPolicyValue(base, forwardPolicyLists[1], "18080")
	if err != nil {
		t.Fatal(err)
	}
	marked, _, err := withDockerForwardMark(ports)
	if err != nil {
		t.Fatal(err)
	}
	reject, err := normalizeAllowRule(allowRule{
		ID: "aaaaaaaa", Kind: "rule", Family: "ipv4", Source: "192.0.2.2/32",
		Protocol: "udp", Ports: "18080", Verdict: "drop",
	})
	if err != nil {
		t.Fatal(err)
	}
	denied, err := replaceManagedRulesIn(inbound, []allowRule{reject}, forwardRuleBlock)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, contents := range map[string]string{
		"default": base, "inbound": inbound, "outbound": outbound, "marked": marked, "denied": denied,
	} {
		if err := os.WriteFile(filepath.Join(dir, name+".nft"), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	script := filepath.Join(dir, "forwarding.sh")
	if err := os.WriteFile(script, forwardingIntegrationScript, 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--net", "sh", script, dir}
	if os.Geteuid() != 0 {
		args = append([]string{"--user", "--map-root-user"}, args...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(ctx, "unshare", args...).CombinedOutput(); err != nil {
		t.Fatalf("isolated nftables test failed: %v\n%s", err, output)
	}
}
