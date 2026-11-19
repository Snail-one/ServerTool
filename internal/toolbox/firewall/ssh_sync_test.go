package firewall

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const activeSSHFirewall = `{"nftables":[
 {"table":{"family":"inet","name":"snail"}},
 {"chain":{"family":"inet","table":"snail","name":"input","type":"filter","hook":"input","policy":"drop"}},
 {"rule":{"family":"inet","table":"snail","chain":"input","handle":40,"comment":"snail:ssh-ports"}},
 {"rule":{"family":"inet","table":"snail","chain":"input","handle":41,"comment":"snail:ssh-ports"}},
 {"rule":{"family":"inet","table":"snail","chain":"input","handle":42,"comment":"manual-rule"}},
 {"rule":{"family":"inet","table":"other","chain":"input","handle":43,"comment":"snail:ssh-ports"}}
]}`

func TestEnsureSSHPortsPreservesMultipleListenersAndUnrelatedRules(t *testing.T) {
	setupTest(t)
	original, err := buildConfig([]int{22, 2200}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	original += "\n# pending edit: keep this content without loading it\n"
	if err := os.WriteFile(configPath, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	var transactions []string
	var validations int
	commandOutput = func(name string, args ...string) (string, error) {
		switch {
		case name == "ss":
			return "LISTEN 0 128 *:22 *:* users:((\"sshd\",pid=1,fd=3))\nLISTEN 0 128 [::]:2200 [::]:* users:((\"sshd\",pid=1,fd=4))", nil
		case reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}):
			return activeSSHFirewall, nil
		case len(args) == 3 && args[0] == "-c" && args[1] == "-f":
			validations++
			return "", nil
		case len(args) == 2 && args[0] == "-f":
			if args[1] == configPath {
				t.Fatal("must not load pending edits or replace unrelated live rules")
			}
			data, err := os.ReadFile(args[1])
			if err != nil {
				t.Fatal(err)
			}
			transactions = append(transactions, string(data))
			return "", nil
		default:
			t.Fatalf("unexpected command: %s %v", name, args)
			return "", nil
		}
	}
	// Repeated updates change only the set, without appending persistent rules.
	for i := 0; i < 2; i++ {
		active, err := EnsureSSHPorts([]int{3022, 4022, 3022})
		if err != nil || !active {
			t.Fatalf("active=%v err=%v", active, err)
		}
	}
	if len(transactions) != 2 || validations != 3 {
		t.Fatalf("transactions=%v validations=%d", transactions, validations)
	}
	want := "delete rule inet snail input handle 40\ndelete rule inet snail input handle 41\n" +
		"insert rule inet snail input udp dport { 22, 2200, 3022, 4022 } counter accept comment \"snail:ssh-ports\"\n" +
		"insert rule inet snail input tcp dport { 22, 2200, 3022, 4022 } counter accept comment \"snail:ssh-ports\"\n"
	if transactions[0] != want || transactions[1] != want {
		t.Fatalf("unexpected transactions: %q", transactions)
	}
	updated, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	expected := strings.ReplaceAll(original, "elements = { 22, 2200, 80, 443 }", "elements = { 80, 443, 3022, 4022 }")
	expected = strings.ReplaceAll(expected, "elements = { 22, 2200 }", "elements = { 3022, 4022 }")
	if string(updated) != expected {
		t.Fatalf("port definitions not updated, or unrelated persistent configuration changed: %s", updated)
	}
	if strings.Contains(string(updated), sshPortsBegin) || strings.Contains(string(updated), sshPortsTag) {
		t.Fatal("transitional old listeners must not be persisted as separate rules")
	}
	backups, err := filepath.Glob(filepath.Join(firewallBackupDir(), "nftables.conf.bak.*"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups=%v err=%v", backups, err)
	}
	if data, err := os.ReadFile(backups[0]); err != nil || string(data) != original {
		t.Fatalf("backup mismatch: %v", err)
	}
	if files, _ := filepath.Glob(filepath.Join(filepath.Dir(configPath), ".snail-*.nft")); len(files) != 0 {
		t.Fatalf("temporary files left: %v", files)
	}
}

func TestEnsureSSHPortsInactiveDoesNotEnableFirewall(t *testing.T) {
	for _, installed := range []bool{false, true} {
		t.Run(map[bool]string{false: "not installed", true: "not loaded"}[installed], func(t *testing.T) {
			setupTest(t)
			commandExists = func(string) bool { return installed }
			commandOutput = func(name string, args ...string) (string, error) {
				if name != "nft" || !reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
					t.Fatalf("unexpected command: %s %v", name, args)
				}
				return `{"nftables":[{"table":{"family":"inet","name":"other"}}]}`, nil
			}
			if active, err := EnsureSSHPorts([]int{2222}); err != nil || active {
				t.Fatalf("active=%v err=%v", active, err)
			}
			if _, err := os.Stat(configPath); !os.IsNotExist(err) {
				t.Fatalf("inactive firewall file created: %v", err)
			}
		})
	}
}

func TestEnsureSSHPortsStopsOnFailuresAndRestoresFile(t *testing.T) {
	for _, failure := range []string{"query", "chain", "listeners", "missing file", "batch validation", "config validation", "load", "concurrent edit"} {
		t.Run(failure, func(t *testing.T) {
			setupTest(t)
			original, _ := buildConfig([]int{22}, false, false)
			if failure != "missing file" {
				if err := os.WriteFile(configPath, []byte(original), 0644); err != nil {
					t.Fatal(err)
				}
			}
			loads := 0
			commandOutput = func(name string, args ...string) (string, error) {
				switch {
				case name == "ss":
					if failure == "listeners" {
						return "", errors.New("no listeners")
					}
					return "LISTEN 0 128 *:22 *:* users:((\"sshd\",pid=1,fd=3))", nil
				case reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}):
					if failure == "query" {
						return "", errors.New("permission denied")
					}
					if failure == "chain" {
						return strings.Replace(activeSSHFirewall, `"hook":"input"`, `"hook":"output"`, 1), nil
					}
					return activeSSHFirewall, nil
				case len(args) == 3 && args[0] == "-c":
					isBatch := strings.Contains(filepath.Base(args[2]), "ssh-ports")
					if (isBatch && failure == "batch validation") || (!isBatch && failure == "config validation") {
						return "bad rule", errors.New("invalid config")
					}
					if !isBatch && failure == "concurrent edit" {
						if err := os.WriteFile(configPath, []byte("# changed by editor\n"), 0644); err != nil {
							t.Fatal(err)
						}
					}
					return "", nil
				case len(args) == 2 && args[0] == "-f":
					loads++
					return "batch rejected", errors.New("atomic transaction failed")
				default:
					t.Fatalf("unexpected command: %s %v", name, args)
					return "", nil
				}
			}
			if active, err := EnsureSSHPorts([]int{3022}); err == nil || active {
				t.Fatalf("must fail: active=%v err=%v", active, err)
			}
			wantLoads := 0
			if failure == "load" {
				wantLoads = 1
			}
			if loads != wantLoads {
				t.Fatalf("loads=%d want=%d", loads, wantLoads)
			}
			if failure != "missing file" {
				want := original
				if failure == "concurrent edit" {
					want = "# changed by editor\n"
				}
				if data, err := os.ReadFile(configPath); err != nil || string(data) != want {
					t.Fatalf("file not preserved: %s %v", data, err)
				}
			}
		})
	}
}

func TestSSHDefinitionsRejectIncompleteConfigurations(t *testing.T) {
	content, _ := buildConfig([]int{22}, false, false)
	updated, err := withSSHPorts(content, []int{22, 3022})
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		updated + "\n" + sshPortsBegin,
		updated + "\n" + sshPortsEnd,
		strings.Replace(content, "set snail_ssh_port {", "set removed_ssh_port {", 1),
		strings.Replace(content, "elements = { 22 }", "22", 1), // Old scalar format requires regeneration.
		strings.Replace(content, "elements = { 22 }", "elements = { 0, 22 }", 1),
		strings.Replace(content, "elements = { 22 }", "elements = { 22, }", 1),
		strings.Replace(content, "set snail_private_ipv6_udp_ports {", "set another_list {", 1),
		strings.Replace(content, "elements = { 22 }", "elements = { $custom_ports }", 1),
		content + "\n    set snail_ssh_port {\n        type inet_service\n        flags interval\n        elements = { 3022 }\n    }\n",
		strings.Replace(content, "chain input {", "chain other {", 1),
		strings.Replace(content, "hook input", "hook output", 1),
		strings.Replace(content, "; policy drop;", "; drop; policy drop;", 1),
	} {
		if _, err := withSSHPorts(bad, []int{4022}); err == nil {
			t.Fatal("incomplete configuration must not be overwritten")
		}
	}
}

func TestSSHDefinitionsReplaceOldPortsAndPreserveBusinessPorts(t *testing.T) {
	content, err := buildConfig([]int{21069}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	content, err = withPolicyValue(content, policyLists[7], "21069, 51820")
	if err != nil {
		t.Fatal(err)
	}
	content = strings.Replace(content, "elements = { 21069, 51820 }", "elements = { 21069, 51820 } # VPN", 1)
	content += "\n# manual note: keep this text\n"
	cleared, err := withPolicyValue(content, policyLists[0], "80, 443")
	if err != nil {
		t.Fatal(err)
	}
	for _, ports := range [][]int{{35678}, {4022, 3022, 4022}, {22333}} {
		updated, err := withSSHPorts(content, ports)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(updated, "21069") || strings.Contains(updated, sshPortsBegin) {
			t.Fatal("old SSH definition or standalone rules retained")
		}
		for _, kept := range []string{"80, 443", "51820", "# VPN", "# manual note: keep this text"} {
			if !strings.Contains(updated, kept) {
				t.Fatalf("business configuration lost: %s", kept)
			}
		}
		if again, err := withSSHPorts(updated, ports); err != nil || again != updated {
			t.Fatalf("repeated update changed configuration: %v", err)
		}
		got, err := sshPortText(updated)
		if err != nil || got != portList(mergeTestPorts(ports)) {
			t.Fatalf("SSH port set = %q, err=%v", got, err)
		}
		withoutSSH, err := withSSHPorts(cleared, ports)
		if err != nil {
			t.Fatal(err)
		}
		public, err := readPolicyValue(withoutSSH, policyLists[0])
		if err != nil || public != "80, 443" {
			t.Fatalf("removed SSH ports were added back: %q %v", public, err)
		}
		content = updated
		cleared, err = withPolicyValue(content, policyLists[0], "80, 443")
		if err != nil {
			t.Fatal(err)
		}
	}
}

func mergeTestPorts(ports []int) []int {
	merged, err := mergeSSHPorts(ports)
	if err != nil {
		return ports
	}
	return merged
}
