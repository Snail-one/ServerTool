package firewall

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

func TestAccessSummaryMergesMatchingPoliciesAndShowsActualSSHPorts(t *testing.T) {
	content, _ := buildConfig([]int{22783}, false, true)
	fields := accessSummaryFields(content)
	if len(fields) != 6 || fields[0].Label != "公网" || fields[0].Value != "IPv4 / IPv6 · TCP / UDP → 22783" ||
		fields[1].Label != "内网" || fields[1].Value != "IPv4 / IPv6 · TCP / UDP → 80, 443, 22783" {
		t.Fatalf("identical policies were not compacted: %v", fields)
	}
	content, _ = withPolicyValue(content, policyLists[7], "51820")
	fields = accessSummaryFields(content)
	want := []ui.CardField{
		{Label: "内网 IPv4", Value: "TCP / UDP → 80, 443, 22783"},
		{Label: "内网 IPv6 TCP", Value: "80, 443, 22783"},
		{Label: "内网 IPv6 UDP", Value: "22783, 51820"},
	}
	if !reflect.DeepEqual(fields[1:4], want) {
		t.Fatalf("different policies were incorrectly merged: %v", fields)
	}
}

func TestAccessManagerSharesOneDraftAndOneSaveAcrossPortsAndSources(t *testing.T) {
	setupTest(t)
	version := ruleTestConfig(t, nil)
	calls := acceptRuleCommands(t)
	query := commandOutput
	commandOutput = func(name string, args ...string) (string, error) {
		if name == "nft" && reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
			return `{"nftables":[]}`, nil
		}
		if len(args) == 3 && args[0] == "-c" {
			pending := shared.ReadFileString(args[2])
			rules, _ := managedRules(pending)
			ports, _ := readPolicyValue(pending, policyLists[4])
			if len(rules) != 1 || rules[0].Ports != "3306" || ports != "8443" {
				t.Fatal("save did not contain the complete shared draft")
			}
			if shared.ReadFileString(configPath) != string(version.content) {
				t.Fatal("a subpage wrote the configuration before the final save")
			}
		}
		return query(name, args...)
	}
	input := "3\n2\n1\n1\n1\n8443\n4\n1\n1\n192.168.1.10\n1\n3306\n1\n\nq\ns\ny\n\nq\n"
	output := capturePortOutput(t, func() {
		if err := Run(inputUI(t, input)); !errors.Is(err, shared.ErrReturnToMenu) {
			t.Fatal(err)
		}
	})
	if !reflect.DeepEqual(*calls, []string{"check", "load"}) {
		t.Fatalf("mixed edits loaded more than once: %v", *calls)
	}
	if strings.Contains(output, "端口与内网设置") || strings.Contains(output, "放行规则管理") || !strings.Contains(output, "访问规则管理") {
		t.Fatal("retired top-level entries are still visible")
	}
	backups, _ := filepath.Glob(filepath.Join(firewallBackupDir(), "nftables.conf.bak.*"))
	if len(backups) != 1 || shared.ReadFileString(backups[0]) != string(version.content) {
		t.Fatal("the shared draft must create exactly one backup of the original")
	}
}

func TestAccessDraftDiscardAndRestoreIncludeSourceChanges(t *testing.T) {
	for _, ending := range []string{"q\n", "r\ns\nq\n", "s\nn\nq\n"} {
		t.Run(ending, func(t *testing.T) {
			setupTest(t)
			version := ruleTestConfig(t, nil)
			input := "2\n\n\n1\n8443\n4\n1\n1\n192.168.1.10\n1\n3306\n1\n\nq\n" + ending
			if err := runRuleManager(inputUI(t, input)); !errors.Is(err, shared.ErrReturnToMenu) {
				t.Fatal(err)
			}
			if shared.ReadFileString(configPath) != string(version.content) {
				t.Fatal("discard/reset wrote a source or port change")
			}
		})
	}
}

func TestNewSourceExceptionsDoNotOfferGlobalPortCreation(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  allowRule
	}{
		{"1\n192.168.1.10\n1\n3306\n1\n", allowRule{ID: "aaaaaaaa", Kind: "source_port", Family: "ipv4", Source: "192.168.1.10", Ports: "3306", Protocol: "tcp"}},
		{"2\n2001:db8::123/32\n2\n3\n", allowRule{ID: "aaaaaaaa", Kind: "source", Family: "ipv6", Source: "2001:db8::/32", Protocol: "both"}},
	} {
		var rule allowRule
		output := capturePortOutput(t, func() {
			var err error
			// Both permissions must remain limited to the source entered first.
			rule, err = askAllowRuleType(inputUI(t, tc.input), allowRule{ID: "aaaaaaaa"}, true)
			if err != nil {
				t.Fatal(err)
			}
		})
		if rule != tc.want || strings.Contains(output, "全来源端口例外") {
			t.Fatalf("source permission was not preserved: got=%+v want=%+v", rule, tc.want)
		}
	}
}

func TestGlobalPortMigrationPreservesOtherRulesAndSelectsCorrectLists(t *testing.T) {
	for _, tc := range []struct {
		family, protocol string
		indices          []int
	}{
		{"ipv4", "tcp", []int{0, 4}},
		{"ipv6", "udp", []int{3, 7}},
		{"both", "both", []int{0, 1, 2, 3, 4, 5, 6, 7}},
		{"", "udp", []int{1, 3, 5, 7}},
	} {
		t.Run(tc.family+tc.protocol, func(t *testing.T) {
			content, _ := buildConfig([]int{2222}, false, false)
			rules := []allowRule{
				{ID: "aaaaaaaa", Kind: "port", Family: tc.family, Ports: "8000-8100", Protocol: tc.protocol},
				{ID: "bbbbbbbb", Kind: "source_port", Source: "192.168.1.10", Ports: "443", Protocol: "tcp"},
			}
			content, _ = replaceManagedRules(content, rules)
			updated, err := migrateGlobalPortRule(content, "aaaaaaaa")
			if err != nil {
				t.Fatal(err)
			}
			remaining, err := managedRules(updated)
			if err != nil || !reflect.DeepEqual(remaining, rules[1:]) {
				t.Fatalf("migration removed or changed unrelated rules: %v %v", remaining, err)
			}
			for i, list := range policyLists[:8] {
				value, err := readPolicyValue(updated, list)
				want := ""
				for _, index := range tc.indices {
					if i == index {
						want = "8000-8100"
					}
				}
				if err != nil || value != want {
					t.Fatalf("%s: got=%q want=%q err=%v", list.name, value, want, err)
				}
			}
			if _, err := migrateGlobalPortRule(content, "bbbbbbbb"); err == nil {
				t.Fatal("source-specific rules must not become global ports")
			}
		})
	}
}

func TestMigrationIsVisibleAndOnlySavedWithTheParentDraft(t *testing.T) {
	setupTest(t)
	rules := []allowRule{{ID: "aaaaaaaa", Kind: "port", Family: "ipv6", Ports: "51820", Protocol: "udp"}}
	version := ruleTestConfig(t, rules)
	calls := acceptRuleCommands(t)
	output := capturePortOutput(t, func() {
		if err := runRuleManager(inputUI(t, "4\n5\n1\n\nq\ns\ny\n")); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "旧任意来源规则") || !strings.Contains(output, "删除放行规则 aaaaaaaa") ||
		!strings.Contains(output, "公网 IPv6 UDP") || !strings.Contains(output, "内网 IPv6 UDP") || !reflect.DeepEqual(*calls, []string{"check", "load"}) {
		t.Fatal("legacy conversion was not reviewable or saved atomically")
	}
	if shared.ReadFileString(configPath) == string(version.content) {
		t.Fatal("migration not saved")
	}
}

func TestSourceEditingStillWorksWithCustomBusinessPortExpressions(t *testing.T) {
	setupTest(t)
	version := ruleTestConfig(t, nil)
	custom := strings.Replace(string(version.content), "$snail_ssh_port }", "$snail_ssh_port, $custom_ports }", 1)
	if err := os.WriteFile(configPath, []byte(custom), 0644); err != nil {
		t.Fatal(err)
	}
	acceptRuleCommands(t)
	if err := runRuleManager(inputUI(t, "4\n1\n1\n192.168.1.10\n1\n3306\n1\n\nq\ns\ny\n")); err != nil {
		t.Fatal(err)
	}
	updated := shared.ReadFileString(configPath)
	if !strings.Contains(updated, "$custom_ports") {
		t.Fatal("source editing changed an unsupported business-port expression")
	}
}
