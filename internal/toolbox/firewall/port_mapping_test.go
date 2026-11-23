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
	calls := acceptRuleCommands(t)
	output := capturePortOutput(t, func() {
		if err := runPortMappingManager(inputUI(t, "1\ntcp 192.0.2.10:8080 192.168.1.20:80\ny\n")); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "端口映射已保存并加载") {
		t.Fatal(output)
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
	for _, input := range []string{
		"1\ntcp 192.0.2.10:8080 192.168.1.20:80\ny\n",
		"3\n1\ntcp 192.0.2.10:8080 192.168.1.21:80 eth0 192.0.2.0/24\ny\n",
		"4\n1\ny\n",
	} {
		capturePortOutput(t, func() {
			if err := runPortMappingManager(inputUI(t, input)); err != nil {
				t.Fatal(err)
			}
		})
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

func TestPortMappingCancelReturnsToList(t *testing.T) {
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

	view := inputUI(t, "2\n0\n2\n1\nq\n0\nnext\n")
	output := capturePortOutput(t, func() {
		if err := runPortMappingManager(view); !errors.Is(err, shared.ErrReturnToMenu) {
			t.Fatalf("expected to exit only from the mapping list: %v", err)
		}
	})
	if strings.Count(output, "已保存的端口映射") != 3 {
		t.Fatal("canceling the selection and confirmation should redraw the mapping list")
	}
	if next, err := view.ReadLine(); err != nil || next != "next" {
		t.Fatalf("unexpected remaining input %q: %v", next, err)
	}
	if got := shared.ReadFileString(configPath); got != initial {
		t.Fatal("canceling the operation must preserve the saved mappings")
	}
}
