package firewall

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/shared"
)

func TestPortMappingsRoundTrip(t *testing.T) {
	original, err := buildConfig([]int{22}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	inputs := [][3]string{
		{"tcp", "192.0.2.10:8080", "192.168.1.20:80"},
		{"udp", "[2001:db8::1]:5353", "[fd00::2]:53"},
	}
	var want []portMapping
	for _, input := range inputs {
		mapping, err := parsePortMapping(input[0], input[1], input[2])
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, mapping)
	}
	config, err := withPortMappings(original, want)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(config, "tcp dport 8080 dnat ip to 192.168.1.20:80") ||
		!strings.Contains(config, "udp dport 5353 dnat ip6 to [fd00::2]:53") {
		t.Fatal("missing DNAT rules")
	}
	got, err := readPortMappings(config)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("mappings: %v, error: %v", got, err)
	}
	if _, err := readPortMappings(strings.Replace(config, "fib daddr type local ", "", 1)); err == nil {
		t.Fatal("should reject a manually changed managed rule")
	}
	removed, err := withPortMappings(config, nil)
	if err != nil || removed != original {
		t.Fatalf("deleting the last mapping did not restore the original config: %v", err)
	}
}

func TestPortMappingManagerSavesAndLoads(t *testing.T) {
	setupTest(t)
	ruleTestConfig(t, nil)
	writeForwardingValue(t, ipv4Forwarding, "1")
	writeForwardingValue(t, ipv6Forwarding, "0")
	calls := acceptRuleCommands(t)
	output := capturePortOutput(t, func() {
		if err := runPortMappingManager(inputUI(t, "n\ntcp 192.0.2.10:8080 192.168.1.20:80\ny\n\n0\n")); !errors.Is(err, shared.ErrReturnToMenu) {
			t.Fatal(err)
		}
	})
	for _, want := range []string{
		"端口映射状态", "内核转发：IPv4 已开启 · IPv6 已关闭", "共 1 条 · 已启用 1 条 · 已停用 0 条",
		"端口映射已保存并加载", "本机端点 → 目标端点", "已启用", "192.0.2.10:8080 → 192.168.1.20:80",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %q in output:\n%s", want, output)
		}
	}
	if !reflect.DeepEqual(*calls, []string{"check", "load"}) {
		t.Fatalf("expected a validation and a load, got %v", *calls)
	}
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	mappings, err := readPortMappings(string(config))
	if err != nil || len(mappings) != 1 {
		t.Fatalf("mappings: %v, error: %v", mappings, err)
	}
}

func TestAdvancedPortMappingRoundTrip(t *testing.T) {
	original, err := buildConfig([]int{22}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	config := portMappingConfig{
		Protocol: "both", From: "*:8080-8082", To: "[fd00::2]:80-82",
		Iface: "eth0", Source: "2001:db8::10/64",
	}
	mapping, err := config.parse()
	if err != nil {
		t.Fatal(err)
	}
	disabled := mapping
	disabled.disabled = true
	withRules, err := withPortMappings(original, []portMapping{mapping, disabled})
	if err != nil {
		t.Fatal(err)
	}
	for _, protocol := range []string{"tcp", "udp"} {
		want := `iifname "eth0" fib daddr type local meta nfproto ipv6 ip6 saddr 2001:db8::/64 ` + protocol +
			` dport 8080-8082 dnat ip6 to [fd00::2] : ` + protocol + ` dport map { 8080 : 80, 8081 : 81, 8082 : 82 }`
		if strings.Count(withRules, want) != 1 {
			t.Fatalf("expected exactly one enabled %s rule", protocol)
		}
	}
	got, err := readPortMappings(withRules)
	if err != nil || !reflect.DeepEqual(got, []portMapping{mapping, disabled}) {
		t.Fatalf("mappings: %v, error: %v", got, err)
	}

	other := mapping
	other.iface = "eth1"
	if mapping.overlaps(other) || mapping.overlaps(disabled) {
		t.Fatal("different interfaces and disabled rules do not overlap")
	}
	other.iface = ""
	other.protocol = "tcp"
	other.from.ports = portInterval{8082, 8083}
	if !mapping.overlaps(other) {
		t.Fatal("wildcard interface and intersecting ports must conflict")
	}
}

func TestReadLegacyPortMapping(t *testing.T) {
	original, err := buildConfig([]int{22}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	mapping, err := parsePortMapping("tcp", "192.0.2.10:8080", "192.168.1.20:80")
	if err != nil {
		t.Fatal(err)
	}
	begin, _, err := portMappingRegion(original)
	if err != nil {
		t.Fatal(err)
	}
	legacy := original[:begin] + portMappingBlock([]portMapping{mapping}, false) + original[begin:]
	got, err := readPortMappings(legacy)
	if err != nil || !reflect.DeepEqual(got, []portMapping{mapping}) {
		t.Fatalf("legacy mapping: %v, error: %v", got, err)
	}
}

func TestEditAndDisablePortMapping(t *testing.T) {
	setupTest(t)
	ruleTestConfig(t, nil)
	calls := acceptRuleCommands(t)
	input := "n\ntcp 192.0.2.10:8080 192.168.1.20:80\ny\n\n" +
		"1\n1\ntcp 192.0.2.10:8080 192.168.1.21:80 eth0 192.0.2.0/24\ny\n\n" +
		"2\ny\n\nq\nq\n"
	output := capturePortOutput(t, func() {
		if err := runPortMappingManager(inputUI(t, input)); !errors.Is(err, shared.ErrReturnToMenu) {
			t.Fatal(err)
		}
	})
	for _, want := range []string{"映射 1", "目标端点：192.168.1.21:80", "入站网卡：eth0", "启用映射"} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing mapping details %q:\n%s", want, output)
		}
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	mappings, err := readPortMappings(string(data))
	if err != nil || len(mappings) != 1 || !mappings[0].disabled || mappings[0].iface != "eth0" ||
		mappings[0].to.String() != "192.168.1.21:80" || strings.Contains(string(data), "dnat ip to") {
		t.Fatalf("disabled mapping: %v, error: %v", mappings, err)
	}
	if !reflect.DeepEqual(*calls, []string{"check", "load", "check", "load", "check", "load"}) {
		t.Fatalf("expected validation and load for each change, got %v", *calls)
	}
}

func TestPortMappingCancelReturnsToPreviousPage(t *testing.T) {
	setupTest(t)
	version := ruleTestConfig(t, nil)
	mapping, err := parsePortMapping("tcp", "*:80", "192.168.1.20:80")
	if err != nil {
		t.Fatal(err)
	}
	initial, err := withPortMappings(string(version.content), []portMapping{mapping})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(initial), 0644); err != nil {
		t.Fatal(err)
	}

	view := inputUI(t, "1\n1\nq\n4\nq\n0\n0\nnext\n")
	output := capturePortOutput(t, func() {
		if err := runPortMappingManager(view); !errors.Is(err, shared.ErrReturnToMenu) {
			t.Fatalf("expected to exit only from the mapping list: %v", err)
		}
	})
	if strings.Count(output, "返回防火墙管理") != 2 {
		t.Fatal("the mapping list should only reappear after leaving the details")
	}
	if strings.Count(output, "返回映射列表") != 3 {
		t.Fatalf("canceling edit and deletion should return to the details:\n%s", output)
	}
	if next, err := view.ReadLine(); err != nil || next != "next" {
		t.Fatalf("unexpected remaining input %q: %v", next, err)
	}
	if got := shared.ReadFileString(configPath); got != initial {
		t.Fatal("canceling the operation must preserve the saved mappings")
	}
}

func TestPortMappingSNATSwitch(t *testing.T) {
	setupTest(t)
	ruleTestConfig(t, nil)
	calls := acceptRuleCommands(t)
	run := func(input string) string {
		t.Helper()
		return capturePortOutput(t, func() {
			if err := runPortMappingManager(inputUI(t, input+"\n0\n")); !errors.Is(err, shared.ErrReturnToMenu) {
				t.Fatal(err)
			}
		})
	}
	read := func() (portMapping, string) {
		t.Helper()
		content := shared.ReadFileString(configPath)
		mappings, err := readPortMappings(content)
		if err != nil || len(mappings) != 1 {
			t.Fatalf("mappings: %v, error: %v", mappings, err)
		}
		return mappings[0], content
	}

	run("n\nudp *:8211 10.77.0.2:8211 eth0 192.0.2.0/24\ny\n")
	output := run("1\n3\nwg0 10.77.0.1\ny\n\nq")
	if !strings.Contains(output, "*:8211 → 10.77.0.2:8211 · SNAT 已开启（wg0，源 IP 10.77.0.1）") {
		t.Fatalf("expected SNAT status after the target endpoint:\n%s", output)
	}
	if !strings.Contains(output, "SNAT 配置：已启用 1 条") {
		t.Fatalf("expected the SNAT count in the status card:\n%s", output)
	}
	mapping, content := read()
	want := `oifname "wg0" ct direction original ct status dnat ip daddr 10.77.0.2 udp dport 8211 ct original ip saddr 192.0.2.0/24 ct original proto-dst 8211 snat ip to 10.77.0.1`
	if !mapping.snatEnabled() || !strings.Contains(content, want) || !strings.Contains(content, "type nat hook postrouting priority 99") {
		t.Fatalf("missing SNAT rule or settings")
	}

	run("1\n3\n1\ny\n\nq")
	mapping, content = read()
	if !mapping.snat.disabled || strings.Contains(content, "hook postrouting") || !strings.Contains(content, "dnat ip to 10.77.0.2:8211") {
		t.Fatal("turning off SNAT must retain its settings and the DNAT rule")
	}

	run("1\n3\n1\ny\n\nq")
	run("1\n2\ny\n\nq")
	mapping, content = read()
	if mapping.snat.disabled || !mapping.disabled || !mapping.snat.address.IsValid() ||
		strings.Contains(content, "hook postrouting") || strings.Contains(content, "dnat ip to") {
		t.Fatal("disabling the mapping must suspend SNAT and preserve its switch")
	}

	run("1\n2\ny\n\nq")
	run("1\n1\nudp *:8211 10.77.0.3:8211 eth0 192.0.2.0/24\ny\n\nq")
	mapping, content = read()
	if !mapping.snatEnabled() || !strings.Contains(content, "ip daddr 10.77.0.3 udp dport 8211") || strings.Contains(content, "10.77.0.2") {
		t.Fatal("editing the target must update the associated SNAT")
	}

	run("1\n4\ny\n")
	if content := shared.ReadFileString(configPath); strings.Contains(content, portMappingsBegin) || strings.Contains(content, "hook postrouting") {
		t.Fatal("deleting the last mapping must also remove its SNAT")
	}
	if len(*calls) != 16 {
		t.Fatalf("expected eight changes, each validated and loaded, got %v", *calls)
	}
}

func TestPortMappingSNATLegacyPriority(t *testing.T) {
	original, err := buildConfig([]int{22}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	mapping, err := (portMappingConfig{
		Protocol: "udp", From: "*:80", To: "10.77.0.2:80",
		SNAT: &portMappingSNATConfig{Iface: "wg0", Address: "10.77.0.1"},
	}).parse()
	if err != nil {
		t.Fatal(err)
	}
	current, err := withPortMappings(original, []portMapping{mapping})
	if err != nil {
		t.Fatal(err)
	}
	legacy := strings.Replace(current, "postrouting priority 99", "postrouting priority 101", 1)
	mappings, err := readPortMappings(legacy)
	if err != nil || !reflect.DeepEqual(mappings, []portMapping{mapping}) {
		t.Fatalf("legacy mappings: %v, error: %v", mappings, err)
	}
	upgraded, err := withPortMappings(legacy, mappings)
	if err != nil || upgraded != current {
		t.Fatalf("saving a legacy mapping must upgrade its SNAT priority: %v", err)
	}
	if _, err := readPortMappings(strings.Replace(legacy, "postrouting priority 101", "postrouting priority 100", 1)); err == nil {
		t.Fatal("unexpected SNAT priorities must still be rejected")
	}
}

func TestPortMappingSNATRejectsAmbiguousIngress(t *testing.T) {
	config := portMappingConfig{
		Protocol: "tcp", From: "192.0.2.10:80", To: "192.168.1.20:80", Iface: "eth0",
		SNAT: &portMappingSNATConfig{Iface: "wg0", Address: "192.168.1.1"},
	}
	first, err := config.parse()
	if err != nil {
		t.Fatal(err)
	}
	config.SNAT = nil
	config.Iface = "eth1"
	second, err := config.parse()
	if err != nil {
		t.Fatal(err)
	}
	if first.overlaps(second) || validatePortMappingSNAT([]portMapping{first, second}) == nil {
		t.Fatal("DNAT can distinguish interfaces at ingress, but the SNAT tuples are ambiguous")
	}
	config.From = "192.0.2.10:8080"
	second, err = config.parse()
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePortMappingSNAT([]portMapping{first, second}); err != nil {
		t.Fatalf("distinct original destination ports should work: %v", err)
	}
}
