package dockermark

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/firewall/settings/counters"
)

func TestRemoveDockerForwardServiceStopsBeforeDeletingAndIsIdempotent(t *testing.T) {
	setupTest(t)
	commandExists = func(name string) bool { return name == "systemctl" }
	if err := os.WriteFile(dockerForwardUnitPath, []byte("unit"), 0600); err != nil {
		t.Fatal(err)
	}
	var calls []string
	commandOutput = func(name string, args ...string) (string, error) {
		command := strings.Join(append([]string{name}, args...), " ")
		calls = append(calls, command)
		switch command {
		case "systemctl disable --now snail-docker-forward.service":
			if !fileExists(dockerForwardUnitPath) {
				t.Fatal("unit was removed before stopping")
			}
		case "systemctl daemon-reload":
			if fileExists(dockerForwardUnitPath) {
				t.Fatal("systemd reloaded before the unit was removed")
			}
		default:
			t.Fatalf("unexpected command: %s", command)
		}
		return "", nil
	}
	for i := 0; i < 2; i++ {
		if err := testService().RemoveService(); err != nil {
			t.Fatal(err)
		}
	}
	if len(calls) != 2 {
		t.Fatalf("unexpected calls: %v", calls)
	}
}

func TestDockerForwardMarkStaysOnForwardAccepts(t *testing.T) {
	content := testConfig()
	content = strings.ReplaceAll(content, "ct mark set ct mark or 0x00800000 ", "")
	updated, changed, err := MarkForwardAccepts(content)
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	if strings.Contains(updated, "ct state established,related "+MarkExpression) {
		t.Fatal("generic established accept was marked")
	}
	if !strings.Contains(updated, "@snail_forward_public_ipv4_tcp_ports "+MarkExpression) {
		t.Fatal("explicit port allow was not marked")
	}
	legacy := strings.Replace(updated, "ct state established,related counter accept\n        jump forward_source_allow",
		"ct state established,related "+MarkExpression+" counter accept\n        jump forward_source_allow", 1)
	if legacy == updated {
		t.Fatal("failed to create legacy rule fixture")
	}
	migrated, changed, err := MarkForwardAccepts(legacy)
	if err != nil || !changed || migrated != updated {
		t.Fatalf("legacy mark was not removed: changed=%v err=%v", changed, err)
	}
	if strings.Contains(updated, "iifname \"lo\" ct mark") || strings.Contains(updated, "hook input priority 0; policy drop;\n\n        # 已建立连接、关联流量及回环\n        ct state established,related ct mark") {
		t.Fatal("input accepts were marked")
	}
	again, changed, err := MarkForwardAccepts(updated)
	if err != nil || changed || again != updated {
		t.Fatal("marking twice changed the config", changed, err)
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
		tcp dport 80 ` + MarkExpression + ` accept; tcp dport 443 accept comment "accept"
		tcp dport 53 counter packets 9 bytes 108 accept
		tcp dport 22 drop
	}
	chain forward_docker { oifname "docker0" accept; }
}
`
	expected := strings.Replace(content, `tcp dport 443 accept comment`, "tcp dport 443 "+MarkExpression+" accept comment", 1)
	expected = strings.Replace(expected, "tcp dport 53 counter", "tcp dport 53 "+MarkExpression+" counter", 1)
	updated, changed, err := MarkForwardAccepts(content)
	if err != nil || !changed || updated != expected {
		t.Fatalf("marked config differs: changed=%v err=%v\n%s", changed, err, updated)
	}
	again, changed, err := MarkForwardAccepts(updated)
	if err != nil || changed || again != updated {
		t.Fatalf("second pass changed the rules: changed=%v err=%v\n%s", changed, err, again)
	}
}

func TestSyncDockerForwardInsertsMissingRuleOnly(t *testing.T) {
	setupTest(t)
	dockerForwardAttempts = 1
	t.Cleanup(func() { dockerForwardAttempts = 30 })
	if err := os.WriteFile(dockerForwardUnitPath, []byte("unit"), 0644); err != nil {
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
	if err := testService().Sync(); err != nil {
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
	content := testConfig()
	content, err := counters.Replace(content, false, func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	previousUnit := dockerForwardUnitPath
	dockerForwardUnitPath = filepath.Join(t.TempDir(), "snail-docker-forward.service")
	t.Cleanup(func() { dockerForwardUnitPath = previousUnit })
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
	if err := testService().enableDockerCompat(); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "meta nfproto ipv4 tcp dport @snail_forward_public_ipv4_tcp_ports "+MarkExpression+" accept") {
		t.Fatal("public forward accept was not marked")
	}
	if enabled, err := counters.Enabled(string(saved)); err != nil || enabled {
		t.Fatalf("enabling Docker compatibility changed packet statistics: enabled=%v err=%v", enabled, err)
	}
	if strings.Contains(string(saved), "iifname \"lo\" ct mark") {
		t.Fatal("input accept was marked")
	}
	unit, err := os.ReadFile(dockerForwardUnitPath)
	if err != nil || !strings.Contains(string(unit), "sync-docker-forward") || !strings.Contains(string(unit), "WantedBy=docker.service") {
		t.Fatalf("unit = %s, err=%v", unit, err)
	}
}

func TestDisablingAndRebuildingDockerMarks(t *testing.T) {
	content := testConfig()
	markedBase, _, err := MarkForwardAccepts(content)
	if err != nil {
		t.Fatal(err)
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
	removed, err := testService().disableDockerCompat()
	if err != nil || !removed {
		t.Fatal(removed, err)
	}
	saved, err := os.ReadFile(configPath)
	if err != nil || strings.Contains(string(saved), MarkExpression) {
		t.Fatalf("disable left the mark in place, err=%v", err)
	}

	if err := os.WriteFile(dockerForwardUnitPath, []byte("unit"), 0644); err != nil {
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
	rebuilt, err := testService().ApplyIfEnabled(content)
	if err != nil || !strings.Contains(rebuilt, "@snail_forward_public_ipv4_tcp_ports "+MarkExpression+" counter accept") {
		t.Fatalf("rebuild while enabled did not mark forward accepts, err=%v", err)
	}
}

func TestDockerCompatRetriesFailedLoadWithLegacyDocker(t *testing.T) {
	setupTest(t)
	content := testConfig()
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
				return strings.ReplaceAll(string(saved), MarkExpression, "ct mark set ct mark | 0x00800000"), err
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

	if err := testService().enableDockerCompat(); err == nil {
		t.Fatal("first load should fail")
	}
	if field := testService().StateField(); field.Value != "规则未同步" {
		t.Fatalf("partially applied state = %+v", field)
	}
	if err := testService().enableDockerCompat(); err != nil {
		t.Fatal(err)
	}
	if loads != 2 || !installed {
		t.Fatalf("loads=%d rule installed=%v", loads, installed)
	}
	if field := testService().StateField(); !strings.Contains(field.Value, "已生效") {
		t.Fatalf("applied state = %+v", field)
	}
	attached = false
	if field := testService().StateField(); field.Value != "等待 Docker 转发链" {
		t.Fatalf("detached status = %+v", field)
	}
	attached, installed = true, false
	if field := testService().StateField(); field.Value != "规则未同步" {
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
	if err := testService().enableDockerCompat(); err == nil || !strings.Contains(err.Error(), "nftables") {
		t.Fatalf("native backend result = %v", err)
	}
}

var (
	configPath, dockerForwardUnitPath, dataDir string
	commandExists                              func(string) bool
	commandOutput                              func(string, ...string) (string, error)
)

func testConfig() string {
	content, _, _ := configuration.RenderTemplate("22", configuration.PortSelection{PublicSSH: true, PrivateSSH: true})
	return content
}

func setupTest(t *testing.T) {
	t.Helper()
	configPath = filepath.Join(t.TempDir(), "nftables.conf")
	dockerForwardUnitPath = filepath.Join(t.TempDir(), "snail-docker-forward.service")
	dataDir = t.TempDir()
	commandExists = func(name string) bool { return name == "nft" }
	commandOutput = func(name string, args ...string) (string, error) {
		t.Fatalf("unexpected command %s %v", name, args)
		return "", nil
	}
}

func testService() Service {
	return Service{
		Store:    configuration.Store{Path: configPath, DataDir: dataDir, CommandOutput: commandOutput, PreserveOriginal: func() error { return nil }},
		UnitPath: dockerForwardUnitPath, IsRoot: func() bool { return true }, CommandExists: commandExists,
		HasManagedConfig: func(content string) bool { return strings.Contains(content, configuration.ManagedBegin) },
	}
}
