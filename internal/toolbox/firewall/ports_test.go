package firewall

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/shared"
)

func portFixture(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("testdata/ports_ruleset.json")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestLoadedPortSummarySeparatesScopeFamilyAndProtocol(t *testing.T) {
	summary, err := summarizeLoadedPorts(portFixture(t))
	if err != nil || summary.problem != "" || !summary.found {
		t.Fatalf("summary failed: %v %s", err, summary.problem)
	}
	want := [2][2][2]string{
		{{"443, 2222, 8000-8002, 8443", "2222, 8443"}, {"2222, 8443", "443, 2222, 8443"}},
		{{"80, 443, 2222, 8443", "53, 2222, 8443"}, {"3333, 8443", "2222, 8443, 51820-51821"}},
	}
	for scope := range want {
		for family := range want[scope] {
			for protocol := range want[scope][family] {
				if got := summary.ports[scope][family][protocol].String(); got != want[scope][family][protocol] {
					t.Fatalf("scope=%d family=%d protocol=%d got=%q want=%q", scope, family, protocol, got, want[scope][family][protocol])
				}
			}
		}
	}
	if !reflect.DeepEqual(summary.otherInput, []string{"ip other / input"}) || len(summary.private[0]) != 3 || len(summary.private[1]) != 2 {
		t.Fatalf("source classification or other-chain scope missing: %+v", summary)
	}
	merged := mergePortExceptions(summary.exceptions)
	if len(merged) != 6 {
		t.Fatalf("exceptions=%d want 6", len(merged))
	}
	var descriptions []string
	for _, rule := range merged {
		descriptions = append(descriptions, rule.description())
	}
	text := strings.Join(descriptions, "\n")
	for _, expected := range []string{"2001:db8::/32 → 443 / TCP+UDP", "192.0.2.10 → 全部端口 / TCP", "203.0.113.10 → 全部端口 / 全部协议", "10.1.1.1 → 全部端口 / TCP+UDP", "仅限源端口 547"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %q in %s", expected, text)
		}
	}
}

func editPortFixture(t *testing.T, edit func([]map[string]any) []map[string]any) string {
	t.Helper()
	objects, err := decodeRules(portFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{"nftables": edit(objects)})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestPortSummaryHonorsRuleOrderAndNamedSetElements(t *testing.T) {
	content := editPortFixture(t, func(objects []map[string]any) []map[string]any {
		for _, object := range objects {
			rule, _ := object["rule"].(map[string]any)
			if rule["handle"] == json.Number("10") {
				objects = append(objects, map[string]any{"element": map[string]any{"family": "inet", "table": "snail", "name": "public_tcp4", "elem": []any{9000}}})
				// Block 8001 before the allowed range. The remaining portions
				// must survive without reporting 8001 as allowed.
				blocked, _ := decodeRules(`{"nftables":[{"rule":{"family":"inet","table":"snail","chain":"public_ports","expr":[{"match":{"op":"==","left":{"payload":{"protocol":"tcp","field":"dport"}},"right":8001}},{"drop":null}]}}]}`)
				objects = append(blocked, objects...)
				break
			}
		}
		return objects
	})
	summary, err := summarizeLoadedPorts(content)
	if err != nil || summary.problem != "" {
		t.Fatalf("summary failed: %v %s", err, summary.problem)
	}
	if got := summary.ports[0][0][0].String(); got != "443, 2222, 8000, 8002, 8443, 9000" {
		t.Fatalf("rule order or named set elements ignored: %s", got)
	}
}

func TestPortSummaryHonorsDefaultAcceptAndDoesNotIncludeUnreachableRules(t *testing.T) {
	for _, scenario := range []string{"default accept", "early terminal drop"} {
		t.Run(scenario, func(t *testing.T) {
			content := editPortFixture(t, func(objects []map[string]any) []map[string]any {
				var result []map[string]any
				if scenario == "early terminal drop" {
					terminal, _ := decodeRules(`{"nftables":[{"rule":{"family":"inet","table":"snail","chain":"public_ports","expr":[{"drop":null}]}}]}`)
					result = append(result, terminal...)
				}
				for _, object := range objects {
					if scenario == "default accept" {
						if chain, ok := object["chain"].(map[string]any); ok && chain["table"] == "snail" && chain["name"] == "input" {
							chain["policy"] = "accept"
						}
						if rule, ok := object["rule"].(map[string]any); ok && (rule["handle"] == json.Number("14") || rule["handle"] == json.Number("24")) {
							continue
						}
					}
					result = append(result, object)
				}
				return result
			})
			summary, err := summarizeLoadedPorts(content)
			if err != nil || summary.problem != "" {
				t.Fatalf("summary failed: %v %s", err, summary.problem)
			}
			want := "全部端口"
			if scenario == "early terminal drop" {
				want = "8443" // Only the earlier global source_allow accept survives.
			}
			for family := range summary.ports[0] {
				for protocol := range summary.ports[0][family] {
					if got := summary.ports[0][family][protocol].String(); got != want {
						t.Fatalf("port policy order was ignored: got=%s want=%s", got, want)
					}
				}
			}
		})
	}
}

func TestUnsupportedOrChangedRoutingNeverClaimsPortsAreClosed(t *testing.T) {
	for _, scenario := range []string{"early drop", "changed goto", "extra source filter", "missing set", "rate limit", "bad policy"} {
		t.Run(scenario, func(t *testing.T) {
			content := editPortFixture(t, func(objects []map[string]any) []map[string]any {
				for _, object := range objects {
					if chain, ok := object["chain"].(map[string]any); ok && chain["name"] == "input" && chain["table"] == "snail" && scenario == "bad policy" {
						chain["policy"] = "unexpected"
					}
					rule, ok := object["rule"].(map[string]any)
					if !ok {
						continue
					}
					if rule["handle"] == json.Number("40") && scenario == "early drop" {
						rule["expr"] = []any{map[string]any{"drop": nil}}
					}
					if rule["handle"] == json.Number("47") && scenario == "changed goto" {
						rule["expr"] = []any{map[string]any{"jump": map[string]any{"target": "public_ports"}}}
					}
					if rule["handle"] == json.Number("10") {
						switch scenario {
						case "missing set":
							rule["expr"].([]any)[1].(map[string]any)["match"].(map[string]any)["right"] = "@missing"
						case "rate limit":
							rule["expr"] = append([]any{map[string]any{"limit": map[string]any{"rate": 1, "per": "second"}}}, rule["expr"].([]any)...)
						case "extra source filter":
							rule["expr"] = append([]any{map[string]any{"match": map[string]any{"op": "==", "left": map[string]any{"payload": map[string]any{"protocol": "ip", "field": "saddr"}}, "right": "192.0.2.10"}}}, rule["expr"].([]any)...)
						}
					}
				}
				return objects
			})
			summary, err := summarizeLoadedPorts(content)
			if err != nil || summary.problem == "" {
				t.Fatalf("unsupported policy must be explicit: %v %+v", err, summary)
			}
		})
	}
}

func TestShowLoadedPortsReadsKernelOnlyAndDisplaysSystemExceptions(t *testing.T) {
	setupTest(t)
	if err := os.WriteFile(configPath, []byte("# un-applied file has TCP 9999\n"), 0600); err != nil {
		t.Fatal(err)
	}
	commandOutput = func(name string, args ...string) (string, error) {
		if name != "nft" || !reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
			t.Fatalf("must only query kernel: %s %v", name, args)
		}
		return portFixture(t), nil
	}
	output := capturePortOutput(t, func() {
		if err := showLoadedPorts(); err != nil {
			t.Fatal(err)
		}
	})
	for _, expected := range []string{"公网 IPv4 TCP", "内网 IPv6 UDP", "8000-8002", "192.0.2.10", "DHCPv6 客户端", "仅限源端口 547", "其他入站链"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("missing %q in %s", expected, output)
		}
	}
	if strings.Contains(output, "9999") {
		t.Fatal("unloaded file content leaked into active-port summary")
	}
	commandOutput = func(string, ...string) (string, error) { return "permission denied", errors.New("failed") }
	if err := showLoadedPorts(); err == nil {
		t.Fatal("query failure must not look like an empty firewall")
	}
}

func TestLoadedPortsMenuUsesReadOnlyKernelQueries(t *testing.T) {
	setupTest(t)
	commandOutput = func(name string, args ...string) (string, error) {
		if name != "nft" || !reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
			t.Fatalf("port view must not validate or apply configuration: %s %v", name, args)
		}
		return portFixture(t), nil
	}
	output := capturePortOutput(t, func() {
		if err := Run(inputUI(t, "6\n\nq\n")); !errors.Is(err, shared.ErrReturnToMenu) {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "公网 IPv4 TCP") || !strings.Contains(output, "DHCPv6 客户端") {
		t.Fatal("menu did not render the loaded-port summary")
	}
}

func capturePortOutput(t *testing.T, run func()) string {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "output-*")
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = file
	defer func() { os.Stdout = previous; _ = file.Close() }()
	run()
	data, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
