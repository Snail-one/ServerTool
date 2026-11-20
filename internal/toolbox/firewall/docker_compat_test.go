package firewall

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerForwardMarkStaysOnForwardAccepts(t *testing.T) {
	content, err := buildConfig([]int{22}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	content = strings.ReplaceAll(content, "ct mark set ct mark or 0x00800000 ", "")
	updated, changed, err := withDockerForwardMark(content)
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	if !strings.Contains(updated, "ct state established,related ct mark set ct mark or 0x00800000 counter accept") {
		t.Fatal("forward established accept was not marked")
	}
	if strings.Contains(updated, "iifname \"lo\" ct mark") || strings.Contains(updated, "hook input priority 0; policy drop;\n\n        # 已建立连接、关联流量及回环\n        ct state established,related ct mark") {
		t.Fatal("input accepts were marked")
	}
	again, changed, err := withDockerForwardMark(updated)
	if err != nil || changed || again != updated {
		t.Fatal("marking twice changed the config", changed, err)
	}
	rules, err := managedRulesIn(updated, forwardRuleBlock)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 0 {
		t.Fatalf("unexpected forward rules: %+v", rules)
	}
}

func TestSyncDockerForwardInsertsMissingRuleOnly(t *testing.T) {
	setupTest(t)
	dockerForwardAttempts = 1
	t.Cleanup(func() { dockerForwardAttempts = 30 })
	var calls []string
	commandExists = func(name string) bool { return name == "iptables" || name == "ip6tables" }
	commandOutput = func(name string, args ...string) (string, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if name == "ip6tables" && args[0] == "-C" {
			return "", nil
		}
		if len(args) > 0 && args[0] == "-C" {
			return "", errors.New("missing")
		}
		return "", nil
	}
	if err := SyncDockerForward(); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(calls, "\n")
	if !strings.Contains(joined, "iptables -I DOCKER-USER 1 -m connmark --mark 0x00800000/0x00800000 -j ACCEPT") {
		t.Fatalf("missing insert: %s", joined)
	}
	if strings.Contains(joined, "ip6tables -I") {
		t.Fatalf("existing IPv6 rule was inserted again: %s", joined)
	}
}

func TestEnableDockerCompatWritesUnitAndMark(t *testing.T) {
	setupTest(t)
	content, err := buildConfig([]int{22}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	content = strings.ReplaceAll(content, "ct mark set ct mark or 0x00800000 ", "")
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	previousUnit := DockerForwardUnitPath
	DockerForwardUnitPath = filepath.Join(t.TempDir(), "snail-docker-forward.service")
	t.Cleanup(func() { DockerForwardUnitPath = previousUnit })
	commandExists = func(string) bool { return true }
	commandOutput = func(name string, args ...string) (string, error) {
		switch name {
		case "docker":
			return "Firewall Backend: iptables\n", nil
		case "nft":
			return "", nil
		case "iptables":
			if args[0] == "-S" {
				return "-N DOCKER-USER\n", nil
			}
			if args[0] == "-C" {
				return "", errors.New("missing")
			}
			return "", nil
		case "ip6tables":
			if args[0] == "-S" {
				return "", errors.New("no chain")
			}
			return "", nil
		case "systemctl":
			return "", nil
		default:
			t.Fatalf("unexpected command %s %v", name, args)
			return "", nil
		}
	}
	if err := enableDockerCompat(); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "meta nfproto ipv4 tcp dport @snail_forward_public_ipv4_tcp_ports ct mark set ct mark or 0x00800000 counter accept") {
		t.Fatal("public forward accept was not marked")
	}
	if strings.Contains(string(saved), "iifname \"lo\" ct mark") {
		t.Fatal("input accept was marked")
	}
	unit, err := os.ReadFile(DockerForwardUnitPath)
	if err != nil || !strings.Contains(string(unit), "sync-docker-forward") || !strings.Contains(string(unit), "WantedBy=docker.service") {
		t.Fatalf("unit = %s, err=%v", unit, err)
	}
}
