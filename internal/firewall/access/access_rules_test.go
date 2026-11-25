package access_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/firewall/access"
	"snail_tool/internal/firewall/configuration/generate"
	rulespkg "snail_tool/internal/firewall/internal/rules"
	"snail_tool/internal/firewall/internal/testenv"
	"snail_tool/internal/storage"
	"snail_tool/internal/ui"
)

func TestAccessSummaryMergesMatchingPoliciesAndShowsActualSSHPorts(t *testing.T) {
	content, _ := generate.Build([]int{22783}, false, true)
	fields := access.SummaryFields(content)
	if len(fields) != 6 || fields[0].Label != "公网" || fields[0].Value != "IPv4 / IPv6 · TCP / UDP → 22783" ||
		fields[1].Label != "内网" || fields[1].Value != "IPv4 / IPv6 · TCP / UDP → 80, 443, 22783" {
		t.Fatalf("identical policies were not compacted: %v", fields)
	}
	content, _ = rulespkg.WithPolicyValue(content, access.Lists[7], "22783, 51820")
	fields = access.SummaryFields(content)
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
	env := testenv.New(t)
	version := env.RuleConfig(t, nil)
	calls := env.AcceptRuleCommands(t)
	query := env.CommandOutput
	env.CommandOutput = func(name string, args ...string) (string, error) {
		if name == "nft" && reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
			return `{"nftables":[]}`, nil
		}
		if len(args) == 3 && args[0] == "-c" {
			pending := storage.ReadFileString(args[2])
			rules, _ := rulespkg.ManagedRules(pending)
			ports, _ := rulespkg.ReadPolicyValue(pending, access.Lists[4])
			if len(rules) != 1 || rules[0].Ports != "3306" || ports != "2222, 8443" {
				t.Fatal("save did not contain the complete shared draft")
			}
			if storage.ReadFileString(env.ConfigPath) != string(version.Content) {
				t.Fatal("a subpage wrote the configuration before the final save")
			}
		}
		return query(name, args...)
	}
	input := "1\n3\n1\n1\n1\n8443\n5\nn\n\n1\n192.168.1.10\n\n\n\n\n3306\n1\n\nq\ns\n\nq\nq\n"
	output := testenv.CaptureOutput(t, func() {
		if err := env.Run(testenv.Input(t, input)); !errors.Is(err, ui.ErrReturnToMenu) {
			t.Fatal(err)
		}
	})
	if !reflect.DeepEqual(*calls, []string{"check", "load"}) {
		t.Fatalf("mixed edits loaded more than once: %v", *calls)
	}
	_, afterSave, saved := strings.Cut(output, "访问规则已保存并加载")
	if !saved || !strings.Contains(afterSave, "按回车返回访问规则管理") ||
		!strings.Contains(afterSave, "访问规则（当前配置）") || strings.Contains(afterSave, "保存并立即生效") {
		t.Fatal("saving should reopen the rule manager with a clean draft")
	}
	if strings.Contains(output, "端口与内网设置") || strings.Contains(output, "放行规则管理") || !strings.Contains(output, "访问规则管理") {
		t.Fatal("retired top-level entries are still visible")
	}
	backups, _ := filepath.Glob(filepath.Join(env.BackupDir(), "nftables.conf.bak.*"))
	if len(backups) != 1 || storage.ReadFileString(backups[0]) != string(version.Content) {
		t.Fatal("the shared draft must create exactly one backup of the original")
	}
}

func TestAccessDraftDiscardAndRestoreIncludeSourceChanges(t *testing.T) {
	for _, ending := range []string{"q\ny\n", "r\ns\nq\n"} {
		t.Run(ending, func(t *testing.T) {
			env := testenv.New(t)
			version := env.RuleConfig(t, nil)
			input := "3\n\n\n1\n8443\n5\nn\n\n1\n192.168.1.10\n\n\n\n\n3306\n1\n\nq\n" + ending
			if err := env.AccessPolicy().Run(testenv.Input(t, input)); !errors.Is(err, ui.ErrReturnToMenu) {
				t.Fatal(err)
			}
			if storage.ReadFileString(env.ConfigPath) != string(version.Content) {
				t.Fatal("discard/reset wrote a source or port change")
			}
		})
	}
}

func TestNewSourceExceptionsDoNotOfferGlobalPortCreation(t *testing.T) {
	env := testenv.New(t)
	for _, tc := range []struct {
		input string
		want  rulespkg.Rule
	}{
		{"\n1\n192.168.1.10\n\n\n\n\n3306\n1\n", rulespkg.Rule{ID: "aaaaaaaa", Kind: "source_port", Family: "ipv4", Source: "192.168.1.10", Ports: "3306", Protocol: "tcp"}},
		{"\n2\n2001:db8::123/32\n\n\n\n\n\n3\n", rulespkg.Rule{ID: "aaaaaaaa", Kind: "source", Family: "ipv6", Source: "2001:db8::/32", Protocol: "both"}},
	} {
		var rule rulespkg.Rule
		output := testenv.CaptureOutput(t, func() {
			var err error
			// Both permissions must remain limited to the source entered first.
			rule, err = env.AccessPolicy().AskAllowRule(testenv.Input(t, tc.input), rulespkg.Rule{ID: "aaaaaaaa"})
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
			content, _ := generate.Build([]int{2222}, false, false)
			rules := []rulespkg.Rule{
				{ID: "aaaaaaaa", Kind: "port", Family: tc.family, Ports: "8000-8100", Protocol: tc.protocol},
				{ID: "bbbbbbbb", Kind: "source_port", Source: "192.168.1.10", Ports: "443", Protocol: "tcp"},
			}
			content, _ = rulespkg.ReplaceManagedRules(content, rules)
			updated, err := access.MigrateGlobalPortRule(content, "aaaaaaaa")
			if err != nil {
				t.Fatal(err)
			}
			remaining, err := rulespkg.ManagedRules(updated)
			if err != nil || !reflect.DeepEqual(remaining, rules[1:]) {
				t.Fatalf("migration removed or changed unrelated rules: %v %v", remaining, err)
			}
			for i, list := range access.Lists[:8] {
				value, err := rulespkg.ReadPolicyValue(updated, list)
				want := "2222"
				for _, index := range tc.indices {
					if i == index {
						want = "2222, 8000-8100"
					}
				}
				if err != nil || value != want {
					t.Fatalf("%s: got=%q want=%q err=%v", list.Name, value, want, err)
				}
			}
			if _, err := access.MigrateGlobalPortRule(content, "bbbbbbbb"); err == nil {
				t.Fatal("source-specific rules must not become global ports")
			}
		})
	}
}

func TestMigrationIsVisibleAndOnlySavedWithTheParentDraft(t *testing.T) {
	env := testenv.New(t)
	rules := []rulespkg.Rule{{ID: "aaaaaaaa", Kind: "port", Family: "ipv6", Ports: "51820", Protocol: "udp"}}
	version := env.RuleConfig(t, rules)
	calls := env.AcceptRuleCommands(t)
	output := testenv.CaptureOutput(t, func() {
		if err := env.AccessPolicy().Run(testenv.Input(t, "5\n1\n3\n\nq\np\n\ns\n\nq\n")); !errors.Is(err, ui.ErrReturnToMenu) {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "旧任意来源规则") || !strings.Contains(output, "删除放行规则 aaaaaaaa") ||
		!strings.Contains(output, "公网 IPv6 UDP") || !strings.Contains(output, "内网 IPv6 UDP") || !reflect.DeepEqual(*calls, []string{"check", "load"}) {
		t.Fatal("legacy conversion was not reviewable or saved atomically")
	}
	if storage.ReadFileString(env.ConfigPath) == string(version.Content) {
		t.Fatal("migration not saved")
	}
}

func TestSourceEditingStillWorksWithCustomBusinessPortExpressions(t *testing.T) {
	env := testenv.New(t)
	version := env.RuleConfig(t, nil)
	custom := strings.Replace(string(version.Content), "elements = { 2222 }", "elements = { 2222, $custom_ports }", 1)
	if err := os.WriteFile(env.ConfigPath, []byte(custom), 0644); err != nil {
		t.Fatal(err)
	}
	env.AcceptRuleCommands(t)
	if err := env.AccessPolicy().Run(testenv.Input(t, "5\nn\n\n1\n192.168.1.10\n\n\n\n\n3306\n1\n\nq\ns\n\nq\n")); !errors.Is(err, ui.ErrReturnToMenu) {
		t.Fatal(err)
	}
	updated := storage.ReadFileString(env.ConfigPath)
	if !strings.Contains(updated, "$custom_ports") {
		t.Fatal("source editing changed an unsupported business-port expression")
	}
}
