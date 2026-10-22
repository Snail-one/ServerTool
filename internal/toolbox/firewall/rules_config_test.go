package firewall

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/shared"
)

func ruleTestConfig(t *testing.T, rules []allowRule) configVersion {
	t.Helper()
	base, err := buildConfig([]int{2222}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	content, err := replaceManagedRules(base, rules)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	version, err := readConfigVersion()
	if err != nil {
		t.Fatal(err)
	}
	return version
}

func TestManagedRulesRoundTripPreservesManualAndBaseConfiguration(t *testing.T) {
	setupTest(t)
	base, _ := buildConfig([]int{2222}, false, false)
	// Also support existing templates that predate the managed rule block.
	base = strings.ReplaceAll(strings.ReplaceAll(base, rulesBegin, ""), rulesEnd, "")
	manual := "        ip saddr 192.0.2.1 tcp dport 8443 counter accept\n"
	base = strings.Replace(base, "chain source_allow {", "chain source_allow {\n"+manual, 1)
	rules := []allowRule{
		{ID: "aaaaaaaa", Kind: "port", Ports: "443", Protocol: "tcp"},
		{ID: "bbbbbbbb", Kind: "source_port", Source: "192.168.1.0/24", Ports: "443", Protocol: "tcp"},
		{ID: "cccccccc", Kind: "source", Source: "2001:db8::/32", Protocol: "both"},
	}
	content, err := replaceManagedRules(base, rules)
	if err != nil {
		t.Fatal(err)
	}
	got, err := managedRules(content)
	if err != nil || !reflect.DeepEqual(got, rules) {
		t.Fatalf("managed rules round trip: %v %v", got, err)
	}
	if !strings.Contains(content, manual) || !strings.Contains(content, "define snail_ssh_port = 2222") || strings.Count(content, rulesBegin) != 1 {
		t.Fatal("manual configuration or SSH baseline changed")
	}
	again, err := replaceManagedRules(content, rules)
	if err != nil || again != content {
		t.Fatal("repeated update must be idempotent")
	}
	if _, err := replaceManagedRules(content, append(rules, rules[0])); err == nil {
		t.Fatal("duplicate IDs must not be saved")
	}
}

func TestManagedRulesRejectsDriftCorruptMarkersAndUnreachableChains(t *testing.T) {
	setupTest(t)
	version := ruleTestConfig(t, []allowRule{{ID: "aaaaaaaa", Kind: "port", Ports: "443", Protocol: "tcp"}})
	base := string(version.content)
	for _, content := range []string{
		strings.Replace(base, "tcp dport 443 counter", "tcp dport 444 counter", 1),
		strings.Replace(base, rulesEnd, "", 1),
		base + "\n" + rulesBegin,
		strings.Replace(base, "jump source_allow", "goto source_allow", 1),
		strings.Replace(base, "chain source_allow {", "chain renamed {", 1),
		strings.Replace(base, "hook input", "hook forward", 1),
		strings.Replace(base, "chain source_allow {", "chain source_allow {\n        return", 1),
		strings.Replace(base, "        jump source_allow", "        ip saddr 192.0.2.1 jump source_allow", 1),
	} {
		if _, err := managedRules(content); err == nil {
			t.Fatal("corrupt or unreachable managed rules accepted")
		}
	}
}

func TestSaveManagedRulesValidatesBeforeWriteAndGuardsConcurrentChanges(t *testing.T) {
	for _, scenario := range []string{"success", "invalid", "changed", "load failure"} {
		t.Run(scenario, func(t *testing.T) {
			setupTest(t)
			version := ruleTestConfig(t, nil)
			wanted := []allowRule{{ID: "aaaaaaaa", Kind: "port", Ports: "8000-8100", Protocol: "both"}}
			var calls []string
			commandOutput = func(name string, args ...string) (string, error) {
				if name != "nft" {
					t.Fatalf("unexpected command: %s", name)
				}
				if len(args) == 3 && args[0] == "-c" && args[1] == "-f" {
					calls = append(calls, "check")
					got, err := managedRules(shared.ReadFileString(args[2]))
					if err != nil || !reflect.DeepEqual(got, wanted) {
						t.Fatalf("wrong proposed rules validated: %v %v", got, err)
					}
					if scenario == "invalid" {
						return "syntax error", errors.New("invalid rules")
					}
					if scenario == "changed" {
						if err := os.WriteFile(configPath, []byte("# concurrent update\n"), 0600); err != nil {
							t.Fatal(err)
						}
					}
					return "", nil
				}
				if reflect.DeepEqual(args, []string{"-f", configPath}) {
					calls = append(calls, "load")
					if scenario == "load failure" {
						return "netlink failure", errors.New("failed")
					}
					return "", nil
				}
				t.Fatalf("unexpected arguments: %v", args)
				return "", nil
			}
			err := saveManagedRules(version, wanted)
			if (err == nil) != (scenario == "success") {
				t.Fatalf("err=%v for %s", err, scenario)
			}
			backups, _ := filepath.Glob(filepath.Join(firewallBackupDir(), "nftables.conf.bak.*"))
			if scenario == "success" || scenario == "load failure" {
				if !reflect.DeepEqual(calls, []string{"check", "load"}) || len(backups) != 1 || shared.ReadFileString(backups[0]) != string(version.content) {
					t.Fatalf("missing validation, load or exact backup: %v %v", calls, backups)
				}
			} else if !reflect.DeepEqual(calls, []string{"check"}) || len(backups) != 0 {
				t.Fatalf("failed validation/concurrency must not write or load: %v %v", calls, backups)
			}
			if scenario == "invalid" && shared.ReadFileString(configPath) != string(version.content) {
				t.Fatal("invalid rules changed the original file")
			}
			if scenario == "changed" && shared.ReadFileString(configPath) != "# concurrent update\n" {
				t.Fatal("concurrent update was overwritten")
			}
		})
	}
}

func TestRegeneratingBaseConfigurationPreservesManagedRules(t *testing.T) {
	setupTest(t)
	wanted := []allowRule{{ID: "aaaaaaaa", Kind: "source_port", Source: "192.168.0.0/16", Ports: "21069", Protocol: "udp"}}
	ruleTestConfig(t, wanted)
	commandOutput = func(name string, args ...string) (string, error) {
		if isSSHDTest(name, args) {
			return sshdTestConfig(2222), nil
		}
		if name == "nft" && len(args) == 3 && args[0] == "-c" && args[1] == "-f" {
			return "", nil
		}
		if name == "nft" && reflect.DeepEqual(args, []string{"-f", configPath}) {
			return "", nil
		}
		t.Fatalf("unexpected command: %s %v", name, args)
		return "", nil
	}
	if err := configure(inputUI(t, "\n\n1\n")); err != nil {
		t.Fatal(err)
	}
	got, err := managedRules(shared.ReadFileString(configPath))
	if err != nil || !reflect.DeepEqual(got, wanted) {
		t.Fatalf("regeneration lost managed rules: %v %v", got, err)
	}
}

func TestFirewallBackupsNeverOverwriteAnEarlierSnapshot(t *testing.T) {
	setupTest(t)
	version := ruleTestConfig(t, nil)
	first, err := backupFirewallConfig()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("# second version\n"), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := backupFirewallConfig()
	if err != nil {
		t.Fatal(err)
	}
	if first == second || shared.ReadFileString(first) != string(version.content) || shared.ReadFileString(second) != "# second version\n" {
		t.Fatal("consecutive edits must preserve distinct complete backups")
	}
}

func TestFirewallBackupUsesPrivateDataDirectory(t *testing.T) {
	setupTest(t)
	const original = "# readable system configuration\n"
	if err := os.WriteFile(configPath, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	// Older backups beside the configuration remain untouched.
	legacy := configPath + ".bak.20261021_120000-old"
	if err := os.WriteFile(legacy, []byte("# historical backup\n"), 0644); err != nil {
		t.Fatal(err)
	}
	backup, err := backupFirewallConfig()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(backup) != firewallBackupDir() || shared.ReadFileString(backup) != original {
		t.Fatalf("backup is not in the tool data directory: %s", backup)
	}
	info, err := os.Stat(backup)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("backup permissions should be 600: %v %v", info, err)
	}
	for _, path := range []string{dataDir, filepath.Join(dataDir, "backups"), firewallBackupDir()} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0700 {
			t.Fatalf("backup directory should be private: %s %v %v", path, info, err)
		}
	}
	oldBackups, err := filepath.Glob(configPath + ".bak.*")
	if err != nil || !reflect.DeepEqual(oldBackups, []string{legacy}) || shared.ReadFileString(legacy) != "# historical backup\n" {
		t.Fatalf("old backups changed or new backup appeared beside configuration: %v %v", oldBackups, err)
	}
	info, err = os.Stat(configPath)
	if err != nil || info.Mode().Perm() != 0644 || shared.ReadFileString(configPath) != original {
		t.Fatalf("system configuration changed during backup: %v %v", info, err)
	}
}

func TestWriteConfigStopsWhenBackupDirectoryIsInvalid(t *testing.T) {
	setupTest(t)
	version := ruleTestConfig(t, nil)
	if err := os.WriteFile(dataDir, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	commandOutput = func(name string, args ...string) (string, error) {
		if name != "nft" || len(args) != 3 || args[0] != "-c" || args[1] != "-f" {
			t.Fatalf("must only validate, never load: %s %v", name, args)
		}
		return "", nil
	}
	if err := writeConfig("# new configuration\n", version); err == nil {
		t.Fatal("backup directory failure should stop writing")
	}
	if shared.ReadFileString(configPath) != string(version.content) {
		t.Fatal("original configuration changed despite failed backup")
	}
}
