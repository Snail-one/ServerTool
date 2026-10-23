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

func TestPolicyPortOperations(t *testing.T) {
	list := policyLists[4]
	for _, tc := range []struct{ current, operation, input, want string }{
		{"80, 8000-8100", "add", "443,8080,8101-8200", "80, 443, 8000-8200"},
		{"80, 443, 8000-8200", "remove", "80,8050-8150", "443, 8000-8049, 8151-8200"},
		{"443", "replace", "1-65535", "1-65535"},
		{"80, 443", "remove", "1-65535", ""},
		{"443", "reset", "", ""},
		{"", "add", "443，80,80", "80, 443"},
	} {
		got, err := changePolicyValue(list, tc.current, tc.operation, tc.input, -1)
		if err != nil || got != tc.want {
			t.Fatalf("%+v: got %q, err=%v", tc, got, err)
		}
	}
	for _, bad := range []string{"0", "65536", "443,", "443,,80", "80; accept", "$other", "90-80", "-1"} {
		if _, err := parsePolicyPorts(bad); err == nil {
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
		{6, "2001:db8::/32", "reset", "", "fc00::/7, fe80::/10", -1},
	} {
		list := policyLists[8]
		if tc.family == 6 {
			list = policyLists[9]
		}
		got, err := changePolicyValue(list, tc.current, tc.operation, tc.input, tc.index)
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
		if _, err := parsePolicySources(tc.input, tc.family); err == nil {
			t.Fatalf("invalid source accepted: %+v", tc)
		}
	}
	if _, err := changePolicyValue(policyLists[8], "10.0.0.0/8", "remove", "10.0.0.0/8", -1); err == nil {
		t.Fatal("removing final entry must not produce an invalid anonymous set")
	}
}

func TestPolicyEditPreservesOtherConfigurationAndSSHLinkage(t *testing.T) {
	content, _ := buildConfig([]int{2222}, true, false)
	content, _ = withPingSetting(content, true)
	content, _ = withPacketCounters(content, false)
	rules := []allowRule{{ID: "aaaaaaaa", Kind: "source_port", Source: "2001:db8::/32", Ports: "8443", Protocol: "tcp", Family: "ipv6"}}
	content, err := replaceManagedRules(content, rules)
	if err != nil {
		t.Fatal(err)
	}
	old := "define snail_private_ipv6_udp_ports = { $snail_ssh_port }"
	multiline := "define snail_private_ipv6_udp_ports = {\n $snail_ssh_port, # SSH\n 51820\n} # VPN"
	content = strings.Replace(content, old, multiline, 1)
	content += "\n# define snail_private_ipv6 = { ::/0 }\ntable inet custom {}\n"
	if err := validatePolicyConfig(content); err != nil {
		t.Fatal(err)
	}
	updated, err := withPolicyValue(content, policyLists[7], "51820, 6000-6010")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(content, multiline, "define snail_private_ipv6_udp_ports = { $snail_ssh_port, 6000-6010, 51820 } # VPN", 1)
	if updated != want {
		t.Fatal("editing one set changed unrelated configuration")
	}
	updated, err = withSSHPorts(updated, []int{3022})
	if err != nil || !strings.Contains(updated, "{ 3022 }") || !strings.Contains(updated, "6000-6010, 51820") {
		t.Fatalf("SSH linkage failed or lost business ports: %v", err)
	}
	if _, err := withSSHPorts(content, []int{3022}); err != nil {
		t.Fatalf("multiline business port lists must support SSH linkage: %v", err)
	}
	withVariable := strings.Replace(content, " 51820\n", " $custom_business_ports\n", 1)
	if _, err := withSSHPorts(withVariable, []int{3022}); err != nil {
		t.Fatalf("SSH linkage must preserve custom business-port expressions: %v", err)
	}
}

func TestPolicyEditorRejectsUnsupportedDefinitions(t *testing.T) {
	base, _ := buildConfig([]int{2222}, false, false)
	for _, bad := range []string{
		strings.Replace(base, "$snail_ssh_port }", "443 }", 1),
		strings.Replace(base, "$snail_ssh_port }", "$snail_ssh_port, }", 1),
		strings.Replace(base, "$snail_ssh_port }", "$snail_ssh_port, $other }", 1),
		strings.Replace(base, "define snail_private_ipv4 =", "define other =", 1),
		strings.Replace(base, "10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16", "::/0", 1),
		base + "\ndefine snail_private_ipv6 = { ::/0 }\n",
		strings.Replace(base, "hook input", "hook output", 1),
	} {
		if err := validatePolicyConfig(bad); err == nil {
			t.Fatal("unsupported or ambiguous configuration accepted")
		}
	}
}

func TestPolicyManagerBatchesDraftAndSavesOnce(t *testing.T) {
	setupTest(t)
	version := ruleTestConfig(t, nil)
	calls := acceptRuleCommands(t)
	// Both private families and both protocols; add, subtract, edit both address lists.
	input := "1\n2\n3\n3\n1\n443,8000-8100\n" +
		"3\n1\n1\n2\n8050-8060\n4\n1\n3\n10.20.1.1/16\n" +
		"4\n2\n5\n2\n2001:db8:1::123/64\ns\ny\n"
	if err := runRuleManager(inputUI(t, input)); err != nil {
		t.Fatal(err)
	}
	updated := shared.ReadFileString(configPath)
	for i, list := range policyLists {
		value, err := readPolicyValue(updated, list)
		want := ""
		switch {
		case i < 4:
			want, _ = readPolicyValue(string(version.content), list)
		case i == 4:
			want = "443, 8000-8049, 8061-8100"
		case i < 8:
			want = "443, 8000-8100"
		case i == 8:
			want = "10.20.0.0/16"
		case i == 9:
			want = "2001:db8:1::/64, fc00::/7"
		}
		if err != nil || value != want {
			t.Fatalf("%s: got=%q want=%q err=%v", list.label, value, want, err)
		}
	}
	if !reflect.DeepEqual(*calls, []string{"check", "load"}) {
		t.Fatalf("draft must validate and load exactly once: %v", *calls)
	}
	backups, _ := filepath.Glob(filepath.Join(firewallBackupDir(), "nftables.conf.bak.*"))
	if len(backups) != 1 || shared.ReadFileString(backups[0]) != string(version.content) {
		t.Fatal("original file not backed up exactly once")
	}
}

func TestPolicyManagerCancellationAndRestoreNeverWrite(t *testing.T) {
	for _, input := range []string{
		"1\nq\nq\n",
		"1\n3\nq\nq\n",
		"3\n1\n1\n1\n8443\nq\n",
		"3\n1\n1\n1\n8443\ns\nn\nq\n",
		"3\n1\n1\n1\n8443\nr\ns\nq\n",
		"3\n3\nq\nq\n",
		"4\n2\n5\nq\nq\n",
		"3\n1\n1\n1\n8443\n", // EOF also discards the draft.
	} {
		t.Run(input, func(t *testing.T) {
			setupTest(t)
			version := ruleTestConfig(t, nil)
			if err := runRuleManager(inputUI(t, input)); err == nil {
				t.Fatal("cancelled/unfinished draft must not report saved")
			}
			if shared.ReadFileString(configPath) != string(version.content) {
				t.Fatal("discarded draft changed the file")
			}
		})
	}
}

func TestPolicyBatchSelectorsTargetOnlyChosenLists(t *testing.T) {
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
		{"1\n1\nb\n2\n\n", []int{2, 3}},
	} {
		got, err := choosePolicyPortLists(inputUI(t, tc.input))
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%q: got=%v want=%v err=%v", tc.input, got, tc.want, err)
		}
	}
}

func TestPolicyEditorRetriesInvalidInputsWithoutChangingOtherLists(t *testing.T) {
	setupTest(t)
	version := ruleTestConfig(t, nil)
	acceptRuleCommands(t)
	input := "3\n1\n1\n1\n65536\n443,,80\n8443\n4\n1\n3\n::1\n10.42.1.1/16\ns\ny\n"
	if err := runRuleManager(inputUI(t, input)); err != nil {
		t.Fatal(err)
	}
	updated := shared.ReadFileString(configPath)
	want, err := withPolicyValue(string(version.content), policyLists[4], "8443")
	if err != nil {
		t.Fatal(err)
	}
	want, err = withPolicyValue(want, policyLists[8], "10.42.0.0/16")
	if err != nil || updated != want {
		t.Fatalf("invalid input leaked into saved draft or changed unrelated values: %v", err)
	}
}

func TestPolicyManagerStopsOnValidationConflictOrLoadFailure(t *testing.T) {
	for _, failure := range []string{"validation", "conflict", "load"} {
		t.Run(failure, func(t *testing.T) {
			setupTest(t)
			version := ruleTestConfig(t, nil)
			loads := 0
			commandOutput = func(name string, args ...string) (string, error) {
				if name != "nft" {
					t.Fatalf("unexpected command: %s", name)
				}
				if len(args) == 3 && args[0] == "-c" {
					if failure == "validation" {
						return "invalid", errors.New("syntax error")
					}
					if failure == "conflict" {
						return "", os.WriteFile(configPath, []byte("# concurrent change\n"), 0644)
					}
					return "", nil
				}
				if reflect.DeepEqual(args, []string{"-f", configPath}) {
					loads++
					return "failed", errors.New("load failed")
				}
				t.Fatalf("unexpected args: %v", args)
				return "", nil
			}
			err := runRuleManager(inputUI(t, "3\n1\n1\n1\n8443\ns\ny\n"))
			if err == nil {
				t.Fatal("expected failure")
			}
			want := string(version.content)
			if failure == "conflict" {
				want = "# concurrent change\n"
			}
			if failure == "load" {
				if loads != 1 || !strings.Contains(err.Error(), "已保存") || !strings.Contains(shared.ReadFileString(configPath), "8443") {
					t.Fatalf("load failure must retain saved file and explain partial result: %v", err)
				}
			} else if loads != 0 || shared.ReadFileString(configPath) != want {
				t.Fatal("validation/conflict failure overwrote config or loaded rules")
			}
		})
	}
}

func TestInitialGenerationSupportsAdvancedPolicyDraft(t *testing.T) {
	setupTest(t)
	loads := 0
	commandOutput = func(name string, args ...string) (string, error) {
		if isSSHDTest(name, args) {
			return sshdTestConfig(2222), nil
		}
		if name == "nft" && len(args) == 3 && args[0] == "-c" {
			return "", nil
		}
		if name == "nft" && reflect.DeepEqual(args, []string{"-f", configPath}) {
			loads++
			return "", nil
		}
		t.Fatalf("unexpected command: %s %v", name, args)
		return "", nil
	}
	// First generation -> custom private IPv6 UDP -> return to final confirmation -> save.
	if err := configure(inputUI(t, "n\nn\n4\n3\n2\n2\n1\n51820\n4\n2\n3\nfd12::/48\ns\ny\n1\n")); err != nil {
		t.Fatal(err)
	}
	content := shared.ReadFileString(configPath)
	value, err := readPolicyValue(content, policyLists[7])
	if err != nil || value != "51820" || loads != 1 || !strings.Contains(content, "define snail_private_ipv6 = { fd12::/48 }") {
		t.Fatalf("custom generation failed: ports=%q loads=%d err=%v", value, loads, err)
	}
}
