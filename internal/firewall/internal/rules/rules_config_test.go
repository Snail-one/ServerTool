package rules_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/firewall/configuration/generate"
	rulespkg "snail_tool/internal/firewall/internal/rules"
	"snail_tool/internal/firewall/internal/testenv"
	"snail_tool/internal/storage"
)

func TestManagedRulesRoundTripPreservesManualAndBaseConfiguration(t *testing.T) {
	base, _ := generate.Build([]int{2222}, false, false)
	// Also support existing templates that predate the managed rule block.
	base = strings.ReplaceAll(strings.ReplaceAll(base, rulespkg.AccessBegin, ""), rulespkg.AccessEnd, "")
	manual := "        ip saddr 192.0.2.1 tcp dport 8443 counter accept\n"
	base = strings.Replace(base, "chain source_allow {", "chain source_allow {\n"+manual, 1)
	rules := []rulespkg.Rule{
		{ID: "aaaaaaaa", Kind: "port", Ports: "443", Protocol: "tcp"},
		{ID: "bbbbbbbb", Kind: "source_port", Source: "192.168.1.0/24", Ports: "443", Protocol: "tcp"},
		{ID: "cccccccc", Kind: "source", Source: "2001:db8::/32", Protocol: "both"},
	}
	content, err := rulespkg.ReplaceManagedRules(base, rules)
	if err != nil {
		t.Fatal(err)
	}
	got, err := rulespkg.ManagedRules(content)
	if err != nil || !reflect.DeepEqual(got, rules) {
		t.Fatalf("managed rules round trip: %v %v", got, err)
	}
	if !strings.Contains(content, manual) || !strings.Contains(content, "elements = { 2222 }") || strings.Count(content, rulespkg.AccessBegin) != 1 {
		t.Fatal("manual configuration or SSH baseline changed")
	}
	again, err := rulespkg.ReplaceManagedRules(content, rules)
	if err != nil || again != content {
		t.Fatal("repeated update must be idempotent")
	}
	if _, err := rulespkg.ReplaceManagedRules(content, append(rules, rules[0])); err == nil {
		t.Fatal("duplicate IDs must not be saved")
	}
}

func TestManagedRulesRejectsDriftCorruptMarkersAndUnreachableChains(t *testing.T) {
	env := testenv.New(t)
	version := env.RuleConfig(t, []rulespkg.Rule{{ID: "aaaaaaaa", Kind: "port", Ports: "443", Protocol: "tcp"}})
	base := string(version.Content)
	for _, content := range []string{
		strings.Replace(base, "tcp dport 443 counter", "tcp dport 444 counter", 1),
		strings.Replace(base, rulespkg.AccessEnd, "", 1),
		base + "\n" + rulespkg.AccessBegin,
		strings.Replace(base, "jump source_allow", "goto source_allow", 1),
		strings.Replace(base, "chain source_allow {", "chain renamed {", 1),
		strings.Replace(base, "hook input", "hook forward", 1),
		strings.Replace(base, "chain source_allow {", "chain source_allow {\n        return", 1),
		strings.Replace(base, "        jump source_allow", "        ip saddr 192.0.2.1 jump source_allow", 1),
	} {
		if _, err := rulespkg.ManagedRules(content); err == nil {
			t.Fatal("corrupt or unreachable managed rules accepted")
		}
	}
}

func TestSaveAccessRulesValidatesBeforeWriteAndGuardsConcurrentChanges(t *testing.T) {
	for _, scenario := range []string{"success", "invalid", "changed", "load failure"} {
		t.Run(scenario, func(t *testing.T) {
			env := testenv.New(t)
			version := env.RuleConfig(t, nil)
			wanted := []rulespkg.Rule{{ID: "aaaaaaaa", Kind: "port", Ports: "8000-8100", Protocol: "both"}}
			var calls []string
			env.CommandOutput = func(name string, args ...string) (string, error) {
				if name != "nft" {
					t.Fatalf("unexpected command: %s", name)
				}
				if len(args) == 3 && args[0] == "-c" && args[1] == "-f" {
					calls = append(calls, "check")
					got, err := rulespkg.ManagedRules(storage.ReadFileString(args[2]))
					if err != nil || !reflect.DeepEqual(got, wanted) {
						t.Fatalf("wrong proposed rules validated: %v %v", got, err)
					}
					if scenario == "invalid" {
						return "syntax error", errors.New("invalid rules")
					}
					if scenario == "changed" {
						if err := os.WriteFile(env.ConfigPath, []byte("# concurrent update\n"), 0600); err != nil {
							t.Fatal(err)
						}
					}
					return "", nil
				}
				if reflect.DeepEqual(args, []string{"-f", env.ConfigPath}) {
					calls = append(calls, "load")
					if scenario == "load failure" {
						return "netlink failure", errors.New("failed")
					}
					return "", nil
				}
				t.Fatalf("unexpected arguments: %v", args)
				return "", nil
			}
			updated, err := rulespkg.ReplaceManagedRules(string(version.Content), wanted)
			if err != nil {
				t.Fatal(err)
			}
			_, err = env.AccessPolicy().SaveRules(version, updated)
			if (err == nil) != (scenario == "success") {
				t.Fatalf("err=%v for %s", err, scenario)
			}
			backups, _ := filepath.Glob(filepath.Join(env.BackupDir(), "nftables.conf.bak.*"))
			if scenario == "success" || scenario == "load failure" {
				if !reflect.DeepEqual(calls, []string{"check", "load"}) || len(backups) != 1 || storage.ReadFileString(backups[0]) != string(version.Content) {
					t.Fatalf("missing validation, load or exact backup: %v %v", calls, backups)
				}
			} else if !reflect.DeepEqual(calls, []string{"check"}) || len(backups) != 0 {
				t.Fatalf("failed validation/concurrency must not write or load: %v %v", calls, backups)
			}
			if scenario == "invalid" && storage.ReadFileString(env.ConfigPath) != string(version.Content) {
				t.Fatal("invalid rules changed the original file")
			}
			if scenario == "changed" && storage.ReadFileString(env.ConfigPath) != "# concurrent update\n" {
				t.Fatal("concurrent update was overwritten")
			}
		})
	}
}
