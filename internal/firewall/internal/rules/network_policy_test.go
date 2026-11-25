package rules_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/firewall/access"
	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/firewall/configuration/generate"
	rulespkg "snail_tool/internal/firewall/internal/rules"
	"snail_tool/internal/firewall/internal/testenv"
	"snail_tool/internal/firewall/settings/ping"
	"snail_tool/internal/firewall/settings/sshaccess"
	"snail_tool/internal/storage"
	"snail_tool/internal/ui"
)

func TestPolicyPortOperations(t *testing.T) {
	list := access.Lists[4]
	for _, tc := range []struct{ current, operation, input, want string }{
		{"80, 8000-8100", "add", "443,8080,8101-8200", "80, 443, 8000-8200"},
		{"80, 443, 8000-8200", "remove", "80,8050-8150", "443, 8000-8049, 8151-8200"},
		{"443", "replace", "1-65535", "1-65535"},
		{"80, 443", "remove", "1-65535", ""},
		{"443", "reset", "", ""},
		{"", "add", "443，80,80", "80, 443"},
	} {
		got, err := rulespkg.ChangePolicyValue(list, tc.current, tc.operation, tc.input, -1)
		if err != nil || got != tc.want {
			t.Fatalf("%+v: got %q, err=%v", tc, got, err)
		}
	}
	for _, bad := range []string{"0", "65536", "443,", "443,,80", "80; accept", "$other", "90-80", "-1"} {
		if _, err := configuration.ParsePorts(bad); err == nil {
			t.Fatalf("invalid port input accepted: %q", bad)
		}
	}
}

func TestPolicySourceOperations(t *testing.T) {
	for _, tc := range []struct {
		family                          int
		current, operation, input, want string
		index                           int
	}{
		{4, "10.0.0.0/8", "add", "192.168.3.1/16,10.1.0.0/16", "10.0.0.0/8, 192.168.0.0/16", -1},
		{4, "10.0.0.0/8, 192.168.0.0/16", "remove", "192.168.1.1/16", "10.0.0.0/8", -1},
		{6, "fc00::/7, fe80::/10", "add", "fd00:1::1/64,2001:db8:2::123/64", "2001:db8:2::/64, fc00::/7, fe80::/10", -1},
		{6, "fc00::/7, fe80::/10", "edit", "2001:db8:1::123/64", "2001:db8:1::/64, fc00::/7", 1},
		{6, "fc00::/7, fe80::/10", "replace", "2001:db8::1,2001:db8::1/128", "2001:db8::1", -1},
		{4, "10.0.0.0/8", "reset", "", "172.16.0.0/12, 192.168.0.0/16", -1},
		{6, "2001:db8::/32", "reset", "", "fc00::/7, fe80::/10", -1},
	} {
		list := access.Lists[8]
		if tc.family == 6 {
			list = access.Lists[9]
		}
		got, err := rulespkg.ChangePolicyValue(list, tc.current, tc.operation, tc.input, tc.index)
		if err != nil || got != tc.want {
			t.Fatalf("%+v: got=%q err=%v", tc, got, err)
		}
	}
	for _, tc := range []struct {
		family int
		input  string
	}{
		{4, "::1"}, {6, "192.168.1.1"}, {6, "::ffff:192.168.1.1"}, {6, "::ffff:192.168.1.0/120"},
		{6, "fe80::1%eth0"}, {4, ""}, {6, "fc00::/7,"}, {4, "10.0.0.0/33"}, {6, "::/129"}, {6, "::1;accept"},
	} {
		if _, err := rulespkg.ParsePolicySources(tc.input, tc.family); err == nil {
			t.Fatalf("invalid source accepted: %+v", tc)
		}
	}
	if _, err := rulespkg.ChangePolicyValue(access.Lists[8], "10.0.0.0/8", "remove", "10.0.0.0/8", -1); err == nil {
		t.Fatal("removing final entry must not produce an invalid anonymous set")
	}
}

func TestPolicyEditPreservesOtherConfigurationAndSSHLinkage(t *testing.T) {
	env := testenv.New(t)
	content, _ := generate.Build([]int{2222}, true, false)
	content, _ = ping.Replace(content, true)
	content, _ = testenv.WithPacketCounters(content, false)
	rules := []rulespkg.Rule{{ID: "aaaaaaaa", Kind: "source_port", Source: "2001:db8::/32", Ports: "8443", Protocol: "tcp", Family: "ipv6"}}
	content, err := rulespkg.ReplaceManagedRules(content, rules)
	if err != nil {
		t.Fatal(err)
	}
	marker := "set snail_private_ipv6_udp_ports {\n        type inet_service\n        flags interval\n        elements = { 2222 }\n    }"
	multiline := "set snail_private_ipv6_udp_ports {\n        type inet_service\n        flags interval\n        elements = { 2222, 51820 }\n    } # VPN"
	content = strings.Replace(content, marker, multiline, 1)
	content += "\n# define snail_private_ipv6 = { ::/0 }\ntable inet custom {}\n"
	if err := env.AccessPolicy().ValidateConfig(content); err != nil {
		t.Fatal(err)
	}
	updated, err := rulespkg.WithPolicyValue(content, access.Lists[7], "2222, 51820, 6000-6010")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(content, "elements = { 2222, 51820 }", "elements = { 2222, 6000-6010, 51820 }", 1)
	if updated != want {
		t.Fatal("editing one set changed unrelated configuration")
	}
	updated, err = sshaccess.ReplacePorts(updated, []int{3022})
	if err != nil || !strings.Contains(updated, "elements = { 3022 }") || !strings.Contains(updated, "3022, 6000-6010, 51820") {
		t.Fatalf("SSH linkage failed or lost business ports: %v", err)
	}
	if _, err := sshaccess.ReplacePorts(content, []int{3022}); err != nil {
		t.Fatalf("multiline business port lists must support SSH linkage: %v", err)
	}
	withVariable := strings.Replace(content, "2222, 51820", "2222, $custom_business_ports", 1)
	if _, err := sshaccess.ReplacePorts(withVariable, []int{3022}); err == nil {
		t.Fatal("named port sets must reject variable references")
	}
}

func TestPolicyEditorRejectsUnsupportedDefinitions(t *testing.T) {
	env := testenv.New(t)
	base, _ := generate.Build([]int{2222}, false, false)
	for _, bad := range []string{
		strings.Replace(base, "set snail_public_ipv4_tcp_ports {\n        type inet_service\n        flags interval\n        elements = { 2222 }", "set snail_public_ipv4_tcp_ports {\n        type inet_service\n        flags interval\n        elements = { $other }", 1),
		strings.Replace(base, "elements = { 2222 }", "elements = { 2222, }", 1),
		strings.Replace(base, "define snail_private_ipv4 =", "define other =", 1),
		strings.Replace(base, "172.16.0.0/12, 192.168.0.0/16", "::/0", 1),
		base + "\ndefine snail_private_ipv6 = { ::/0 }\n",
		strings.Replace(base, "hook input", "hook output", 1),
	} {
		if err := env.AccessPolicy().ValidateConfig(bad); err == nil {
			t.Fatal("unsupported or ambiguous configuration accepted")
		}
	}
}

func TestPolicyManagerBatchesDraftAndSavesOnce(t *testing.T) {
	env := testenv.New(t)
	version := env.RuleConfig(t, nil)
	calls := env.AcceptRuleCommands(t)
	// Add to both public/private policies, subtract only from private IPv4 TCP,
	// then edit both address lists and save the complete draft once.
	input := "1\n3\n3\n1\n443,8000-8100\n" +
		"3\n1\n1\n2\n8050-8060\nl\n1\n3\n10.20.1.1/16\n" +
		"L\n2\n5\n2\n2001:db8:1::123/64\ns\n"
	if err := env.AccessPolicy().Run(testenv.Input(t, input+"\nq\n")); !errors.Is(err, ui.ErrReturnToMenu) {
		t.Fatal(err)
	}
	updated := storage.ReadFileString(env.ConfigPath)
	for i, list := range access.Lists {
		value, err := rulespkg.ReadPolicyValue(updated, list)
		want := ""
		switch {
		case i == 4:
			want = "443, 2222, 8000-8049, 8061-8100"
		case i < 8:
			want = "443, 2222, 8000-8100"
		case i == 8:
			want = "10.20.0.0/16"
		case i == 9:
			want = "2001:db8:1::/64, fc00::/7"
		}
		if err != nil || value != want {
			t.Fatalf("%s: got=%q want=%q err=%v", list.Label, value, want, err)
		}
	}
	if !reflect.DeepEqual(*calls, []string{"check", "load"}) {
		t.Fatalf("draft must validate and load exactly once: %v", *calls)
	}
	backups, _ := filepath.Glob(filepath.Join(env.BackupDir(), "nftables.conf.bak.*"))
	if len(backups) != 1 || storage.ReadFileString(backups[0]) != string(version.Content) {
		t.Fatal("original file not backed up exactly once")
	}
}

func TestPolicyManagerCancellationAndRestoreNeverWrite(t *testing.T) {
	for _, input := range []string{
		"1\nq\nq\n",
		"1\n3\nq\nq\n",
		"3\n1\n1\n1\n8443\nq\n",
		"3\n1\n1\n1\n8443\nr\ns\nq\n",
		"3\n3\nq\nq\n",
		"l\n2\n5\nq\nq\n",
		"3\n1\n1\n1\n8443\n", // EOF also discards the draft.
	} {
		t.Run(input, func(t *testing.T) {
			env := testenv.New(t)
			version := env.RuleConfig(t, nil)
			if err := env.AccessPolicy().Run(testenv.Input(t, input)); err == nil {
				t.Fatal("cancelled/unfinished draft must not report saved")
			}
			if storage.ReadFileString(env.ConfigPath) != string(version.Content) {
				t.Fatal("discarded draft changed the file")
			}
		})
	}
}

func TestPolicyBatchSelectorsTargetOnlyChosenLists(t *testing.T) {
	env := testenv.New(t)
	for _, tc := range []struct {
		input string
		want  []int
	}{
		{"1\n1\n1\n", []int{0}},
		{"1\n2\n2\n", []int{3}},
		{"2\n2\n1\n", []int{6}},
		{"2\n3\n3\n", []int{4, 5, 6, 7}},
		{"3\n1\n2\n", []int{1, 5}},
		{"3\n3\n3\n", []int{0, 1, 2, 3, 4, 5, 6, 7}},
		{"2\n\n\n", []int{4, 5, 6, 7}},
		{"1\n1\nq\n2\n\n", []int{2, 3}},
		{"1\n1\n0\n2\n\n", []int{2, 3}},
	} {
		got, err := env.AccessPolicy().PromptPolicyPortScope(testenv.Input(t, tc.input[2:]), tc.input[:1], nil)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%q: got=%v want=%v err=%v", tc.input, got, tc.want, err)
		}
	}
}

func TestPolicyEditorRetriesInvalidInputsWithoutChangingOtherLists(t *testing.T) {
	env := testenv.New(t)
	version := env.RuleConfig(t, nil)
	env.AcceptRuleCommands(t)
	input := "3\n1\n1\n1\n65536\n443,,80\n8443\nl\n1\n3\n::1\n10.42.1.1/16\ns\n"
	if err := env.AccessPolicy().Run(testenv.Input(t, input+"\nq\n")); !errors.Is(err, ui.ErrReturnToMenu) {
		t.Fatal(err)
	}
	updated := storage.ReadFileString(env.ConfigPath)
	want, err := rulespkg.WithPolicyValue(string(version.Content), access.Lists[4], "2222, 8443")
	if err != nil {
		t.Fatal(err)
	}
	want, err = rulespkg.WithPolicyValue(want, access.Lists[8], "10.42.0.0/16")
	if err != nil || updated != want {
		t.Fatalf("invalid input leaked into saved draft or changed unrelated values: %v", err)
	}
}

func TestPolicyManagerPreservesDraftOrSavedConfigOnFailure(t *testing.T) {
	for _, failure := range []string{"validation", "conflict", "load"} {
		t.Run(failure, func(t *testing.T) {
			env := testenv.New(t)
			version := env.RuleConfig(t, nil)
			loads := 0
			env.CommandOutput = func(name string, args ...string) (string, error) {
				if name != "nft" {
					t.Fatalf("unexpected command: %s", name)
				}
				if len(args) == 3 && args[0] == "-c" {
					if failure == "validation" {
						return "invalid", errors.New("syntax error")
					}
					if failure == "conflict" {
						return "", os.WriteFile(env.ConfigPath, []byte("# concurrent change\n"), 0644)
					}
					return "", nil
				}
				if reflect.DeepEqual(args, []string{"-f", env.ConfigPath}) {
					loads++
					return "failed", errors.New("load failed")
				}
				t.Fatalf("unexpected args: %v", args)
				return "", nil
			}
			view := testenv.Input(t, "3\n1\n1\n1\n8443\ns\nq\ny\n")
			var err error
			output := testenv.CaptureOutput(t, func() {
				err = env.AccessPolicy().Run(view)
			})
			want := string(version.Content)
			if failure == "conflict" {
				want = "# concurrent change\n"
			}
			if failure == "load" {
				if loads != 1 || err == nil || !strings.Contains(err.Error(), "已保存") || !strings.Contains(storage.ReadFileString(env.ConfigPath), "8443") {
					t.Fatalf("load failure must retain saved file and explain partial result: %v", err)
				}
			} else {
				_, retained, shown := strings.Cut(output, "保存失败，草稿已保留")
				if !errors.Is(err, ui.ErrReturnToMenu) || !shown || !strings.Contains(retained, "是否放弃修改") {
					t.Fatalf("failed save must return to the draft before discarding: %v\n%s", err, output)
				}
				if strings.Count(output, "内网 IPv4 TCP：2222, 8443") != 2 {
					t.Fatalf("draft changes must appear before and after failed save:\n%s", output)
				}
				if loads != 0 || storage.ReadFileString(env.ConfigPath) != want {
					t.Fatal("validation/conflict failure overwrote config or loaded rules")
				}
			}
		})
	}
}

func TestPolicyManagerCanRetryRetainedDraft(t *testing.T) {
	env := testenv.New(t)
	version := env.RuleConfig(t, nil)
	calls := env.AcceptRuleCommands(t)
	query := env.CommandOutput
	env.CommandOutput = func(name string, args ...string) (string, error) {
		output, err := query(name, args...)
		if reflect.DeepEqual(*calls, []string{"check"}) {
			return "invalid", errors.New("temporary validation failure")
		}
		return output, err
	}

	view := testenv.Input(t, "3\n1\n1\n1\n8443\ns\ns\n\nq\n")
	if err := env.AccessPolicy().Run(view); !errors.Is(err, ui.ErrReturnToMenu) {
		t.Fatalf("retry did not save and return to the editor: %v", err)
	}
	want, err := rulespkg.WithPolicyValue(string(version.Content), access.Lists[4], "2222, 8443")
	if err != nil || storage.ReadFileString(env.ConfigPath) != want {
		t.Fatalf("retry did not save the retained draft: %v", err)
	}
	if !reflect.DeepEqual(*calls, []string{"check", "check", "load"}) {
		t.Fatalf("expected two validations and one load, got %v", *calls)
	}
}
