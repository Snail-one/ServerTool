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
	if strings.Contains(updated, "ct state established,related "+dockerForwardMarkExpr) {
		t.Fatal("generic established accept was marked")
	}
	if !strings.Contains(updated, "@snail_forward_public_ipv4_tcp_ports "+dockerForwardMarkExpr) {
		t.Fatal("explicit port allow was not marked")
	}
	legacy := strings.Replace(updated, "ct state established,related counter accept\n        jump forward_source_allow",
		"ct state established,related "+dockerForwardMarkExpr+" counter accept\n        jump forward_source_allow", 1)
	if legacy == updated {
		t.Fatal("failed to create legacy rule fixture")
	}
	migrated, changed, err := withDockerForwardMark(legacy)
	if err != nil || !changed || migrated != updated {
		t.Fatalf("legacy mark was not removed: changed=%v err=%v", changed, err)
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

func TestDockerForwardMarkOnlyEditsForwardAcceptVerdicts(t *testing.T) {
	content := `table inet other {
	chain forward { tcp dport 80 counter accept; }
}
table inet snail {
	chain forward {
		type filter hook forward priority -10; policy accept;
	}
	chain forward_public_ports {
		# accept this comment unchanged
		meta l4proto vmap { tcp : accept, udp : drop }
		tcp dport 80 ` + dockerForwardMarkExpr + ` accept; tcp dport 443 accept comment "accept"
		tcp dport 53 counter packets 9 bytes 108 accept
		tcp dport 22 drop
	}
	chain forward_docker { oifname "docker0" accept; }
}
`
	expected := strings.Replace(content, `tcp dport 443 accept comment`, "tcp dport 443 "+dockerForwardMarkExpr+" accept comment", 1)
	expected = strings.Replace(expected, "tcp dport 53 counter", "tcp dport 53 "+dockerForwardMarkExpr+" counter", 1)
	updated, changed, err := withDockerForwardMark(content)
	if err != nil || !changed || updated != expected {
		t.Fatalf("marked config differs: changed=%v err=%v\n%s", changed, err, updated)
	}
	again, changed, err := withDockerForwardMark(updated)
	if err != nil || changed || again != updated {
		t.Fatalf("second pass changed the rules: changed=%v err=%v\n%s", changed, err, again)
	}
}

func TestSyncDockerForwardInsertsMissingRuleOnly(t *testing.T) {
	setupTest(t)
	dockerForwardAttempts = 1
	t.Cleanup(func() { dockerForwardAttempts = 30 })
	if err := os.WriteFile(DockerForwardUnitPath, []byte("unit"), 0644); err != nil {
		t.Fatal(err)
	}
	var calls []string
	commandExists = func(name string) bool {
		return name == "iptables" || name == "ip6tables" || name == "systemctl"
	}
	commandOutput = func(name string, args ...string) (string, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		switch {
		case name == "systemctl":
			return "enabled", nil
		case args[0] == "-S" && args[1] == "FORWARD":
			return "-A FORWARD -j DOCKER-USER\n", nil
		case args[0] == "-S" && args[1] == "DOCKER-USER":
			if name == "ip6tables" {
				return "-A DOCKER-USER " + strings.Join(dockerForwardRuleArgs(), " ") + "\n", nil
			}
			return "-A DOCKER-USER -m connmark --mark 0x800000/0x800000 -j ACCEPT\n" +
				"-A DOCKER-USER -s 192.0.2.1/32 -j DROP\n-A DOCKER-USER -j RETURN\n", nil
		case args[0] == "-I" || args[0] == "-D":
			return "", nil
		default:
			t.Fatalf("unexpected command %s %v", name, args)
			return "", nil
		}
	}
	if err := SyncDockerForward(); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(calls, "\n")
	if !strings.Contains(joined, "iptables -D DOCKER-USER -m connmark --mark 0x00800000/0x00800000 -j ACCEPT") ||
		!strings.Contains(joined, "iptables -I DOCKER-USER 2 "+strings.Join(dockerForwardRuleArgs(), " ")) {
		t.Fatalf("legacy rule was not moved behind the user drop: %s", joined)
	}
	if strings.Contains(joined, "ip6tables -I") || strings.Contains(joined, "ip6tables -D") {
		t.Fatalf("existing IPv6 rule was changed: %s", joined)
	}
}

func TestEnableDockerCompatWritesUnitAndMarkWithCountersOff(t *testing.T) {
	setupTest(t)
	content, err := buildConfig([]int{22}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	content, err = withPacketCounters(content, false)
	if err != nil {
		t.Fatal(err)
	}
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
			if args[0] == "-S" && args[1] == "FORWARD" {
				return "-A FORWARD -j DOCKER-USER\n", nil
			}
			if args[0] == "-S" {
				return "-N DOCKER-USER\n", nil
			}
			return "", nil
		case "ip6tables":
			return "-P FORWARD ACCEPT\n", nil
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
	if !strings.Contains(string(saved), "meta nfproto ipv4 tcp dport @snail_forward_public_ipv4_tcp_ports "+dockerForwardMarkExpr+" accept") {
		t.Fatal("public forward accept was not marked")
	}
	if enabled, err := packetCountersEnabled(string(saved)); err != nil || enabled {
		t.Fatalf("enabling Docker compatibility changed packet statistics: enabled=%v err=%v", enabled, err)
	}
	rule := allowRule{ID: "aaaaaaaa", Kind: "source_port", Family: "ipv4", Source: "192.168.1.10", Ports: "443", Protocol: "tcp"}
	rule, err = normalizeAllowRule(rule)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := replaceManagedRulesIn(string(saved), []allowRule{rule}, forwardRuleBlock)
	if err != nil || !strings.Contains(updated, dockerForwardMarkExpr+" accept comment") {
		t.Fatalf("saving a new forward rule with statistics off lost the mark: %v", err)
	}
	if strings.Contains(string(saved), "iifname \"lo\" ct mark") {
		t.Fatal("input accept was marked")
	}
	unit, err := os.ReadFile(DockerForwardUnitPath)
	if err != nil || !strings.Contains(string(unit), "sync-docker-forward") || !strings.Contains(string(unit), "WantedBy=docker.service") {
		t.Fatalf("unit = %s, err=%v", unit, err)
	}
}

func TestDockerMarkExistsOnlyWhileCompatIsOn(t *testing.T) {
	content, err := buildConfig([]int{22}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(content, dockerForwardMarkExpr) {
		t.Fatal("generated forward rules are marked while Docker compat is off")
	}
	rule := allowRule{ID: "aaaaaaaa", Kind: "source_port", Family: "ipv4", Source: "192.168.1.10", Ports: "443", Protocol: "tcp"}
	rule, err = normalizeAllowRule(rule)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := replaceManagedRulesIn(content, []allowRule{rule}, forwardRuleBlock)
	if err != nil || strings.Contains(plain, dockerForwardMarkExpr) {
		t.Fatalf("unmarked forward edit gained a mark, err=%v", err)
	}
	markedBase, changed, err := withDockerForwardMark(content)
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	marked, err := replaceManagedRulesIn(markedBase, []allowRule{rule}, forwardRuleBlock)
	if err != nil || !strings.Contains(marked, dockerForwardMarkExpr+" counter accept comment") {
		t.Fatalf("enabled forward edit lost the mark, err=%v", err)
	}

	setupTest(t)
	if err := os.WriteFile(configPath, []byte(markedBase), 0600); err != nil {
		t.Fatal(err)
	}
	commandExists = func(name string) bool { return name == "nft" || name == "iptables" }
	commandOutput = func(name string, args ...string) (string, error) {
		if name == "nft" {
			return "", nil
		}
		if name == "iptables" && len(args) > 0 && args[0] == "-S" {
			return "-P FORWARD ACCEPT\n", nil
		}
		t.Fatalf("unexpected command %s %v", name, args)
		return "", errors.New("unexpected")
	}
	removed, err := disableDockerCompat()
	if err != nil || !removed {
		t.Fatal(removed, err)
	}
	saved, err := os.ReadFile(configPath)
	if err != nil || strings.Contains(string(saved), dockerForwardMarkExpr) {
		t.Fatalf("disable left the mark in place, err=%v", err)
	}

	if err := os.WriteFile(DockerForwardUnitPath, []byte("unit"), 0644); err != nil {
		t.Fatal(err)
	}
	commandExists = func(name string) bool { return name == "systemctl" }
	commandOutput = func(name string, args ...string) (string, error) {
		if name == "systemctl" && len(args) > 0 && args[0] == "is-enabled" {
			return "enabled", nil
		}
		t.Fatalf("unexpected command %s %v", name, args)
		return "", errors.New("unexpected")
	}
	rebuilt, err := applyDockerForwardMarkIfEnabled(content)
	if err != nil || !strings.Contains(rebuilt, "@snail_forward_public_ipv4_tcp_ports "+dockerForwardMarkExpr+" counter accept") {
		t.Fatalf("rebuild while enabled did not mark forward accepts, err=%v", err)
	}
}

func TestDockerCompatRetriesFailedLoadWithLegacyDocker(t *testing.T) {
	setupTest(t)
	content, err := buildConfig([]int{22}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	loads := 0
	installed, attached := false, true
	commandExists = func(name string) bool { return name != "ip6tables" }
	commandOutput = func(name string, args ...string) (string, error) {
		switch name {
		case "docker":
			return "Server Version: 28.5.2\n", nil
		case "nft":
			switch args[0] {
			case "-f":
				loads++
				if loads == 1 {
					return "load failed", errors.New("nft failure")
				}
			case "-nn":
				if loads == 1 {
					return content, nil
				}
				saved, err := os.ReadFile(configPath)
				return strings.ReplaceAll(string(saved), dockerForwardMarkExpr, "ct mark set ct mark | 0x00800000"), err
			}
		case "systemctl":
			if args[0] == "is-enabled" {
				return "enabled", nil
			}
		case "iptables":
			switch {
			case args[0] == "-S" && args[1] == "FORWARD":
				if attached {
					return "-A FORWARD -j DOCKER-USER\n", nil
				}
				return "-P FORWARD DROP\n", nil
			case args[0] == "-S" && args[1] == "DOCKER-USER":
				if installed {
					return "-A DOCKER-USER " + strings.Join(dockerForwardRuleArgs(), " ") + "\n", nil
				}
			case args[0] == "-I":
				installed = true
			}
		default:
			t.Fatalf("unexpected command %s %v", name, args)
		}
		return "", nil
	}

	if err := enableDockerCompat(); err == nil {
		t.Fatal("first load should fail")
	}
	if field := dockerCompatStateField(); field.Value != "规则未同步" {
		t.Fatalf("partially applied state = %+v", field)
	}
	if err := enableDockerCompat(); err != nil {
		t.Fatal(err)
	}
	if loads != 2 || !installed {
		t.Fatalf("loads=%d rule installed=%v", loads, installed)
	}
	if field := dockerCompatStateField(); !strings.Contains(field.Value, "已生效") {
		t.Fatalf("applied state = %+v", field)
	}
	attached = false
	if field := dockerCompatStateField(); field.Value != "等待 Docker 转发链" {
		t.Fatalf("detached status = %+v", field)
	}
	attached, installed = true, false
	if field := dockerCompatStateField(); field.Value != "规则未同步" {
		t.Fatalf("missing rule status = %+v", field)
	}
}

func TestDockerCompatRejectsConfirmedNativeBackend(t *testing.T) {
	setupTest(t)
	commandExists = func(string) bool { return true }
	commandOutput = func(name string, args ...string) (string, error) {
		if name == "docker" {
			return "Firewall Backend: nftables\n", nil
		}
		t.Fatalf("unexpected command %s %v", name, args)
		return "", nil
	}
	if err := enableDockerCompat(); err == nil || !strings.Contains(err.Error(), "nftables") {
		t.Fatalf("native backend result = %v", err)
	}
}
