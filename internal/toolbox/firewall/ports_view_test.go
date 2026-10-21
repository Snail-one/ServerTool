package firewall

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestCompactPortRowsPreserveEveryFamilyProtocolCombination(t *testing.T) {
	// All partitions of four rows into up to four distinct port values,
	// including L-shaped equal subsets, must reconstruct without widening.
	for pattern := 0; pattern < 256; pattern++ {
		summary := loadedPortSummary{}
		summary.private = [2][]string{{"10.0.0.0/8"}, {"fc00::/7"}}
		for scope := range summary.ports {
			for i := 0; i < 4; i++ {
				port := 1000 + ((pattern >> (i * 2)) & 3)
				summary.ports[scope][i/2][i%2] = portIntervals{{port, port}}
			}
		}
		var visited [2][2][2]bool
		for _, field := range compactPortFields(summary) {
			scope := 0
			if strings.HasPrefix(field.Label, "内网") {
				scope = 1
			}
			for family, version := range []string{"IPv4", "IPv6"} {
				for protocol, name := range []string{"TCP", "UDP"} {
					if !strings.Contains(field.Label, version) || !strings.Contains(field.Label, name) {
						continue
					}
					if visited[scope][family][protocol] || field.Value != summary.ports[scope][family][protocol].String() {
						t.Fatalf("pattern=%d incorrectly merged or repeated row: %+v", pattern, field)
					}
					visited[scope][family][protocol] = true
				}
			}
		}
		for scope := range visited {
			for family := range visited[scope] {
				for protocol, found := range visited[scope][family] {
					if !found {
						t.Fatalf("pattern=%d omitted %d/%d/%d", pattern, scope, family, protocol)
					}
				}
			}
		}
	}
}

func TestPortViewDoesNotRepeatGlobalPortsAndRetainsSourceExceptions(t *testing.T) {
	setupTest(t)
	content := editPortFixture(t, func(objects []map[string]any) []map[string]any {
		for _, object := range objects {
			rule, ok := object["rule"].(map[string]any)
			if !ok || rule["family"] != "inet" || rule["table"] != "snail" {
				continue
			}
			// Give all four rows the same base port, mirroring the reported UI.
			if rule["chain"] == "public_ports" || rule["chain"] == "private_ports" {
				handle := fmt.Sprint(rule["handle"])
				if handle == "14" || handle == "24" {
					continue
				}
				for _, raw := range rule["expr"].([]any) {
					expression := raw.(map[string]any)
					match, ok := expression["match"].(map[string]any)
					if !ok {
						continue
					}
					left := match["left"].(map[string]any)
					if payload, ok := left["payload"].(map[string]any); ok && payload["field"] == "dport" {
						match["right"] = json.Number("21069")
					}
				}
			}
		}
		return objects
	})
	commandOutput = func(string, ...string) (string, error) { return content, nil }
	output := capturePortOutput(t, func() {
		if err := showLoadedPorts(); err != nil {
			t.Fatal(err)
		}
	})
	for _, expected := range []string{"公网 IPv4 / IPv6 TCP+UDP", "内网 IPv4 / IPv6 TCP+UDP", "8443, 21069", "abcd0001", "192.0.2.10", "2001:db8::/32", "DHCPv6 客户端", "仅限源端口 547"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("missing %q: %s", expected, output)
		}
	}
	if strings.Count(output, "╭─ ServerTool") != 2 || strings.Contains(output, "任意来源 → 8443") {
		t.Fatalf("must use one card and avoid repeating global accepts: %s", output)
	}
}

func TestCompactPortRowsDoNotMergeMissingPrivateSourcesIntoAllowedPorts(t *testing.T) {
	summary := loadedPortSummary{private: [2][]string{{"10.0.0.0/8"}, nil}}
	for scope := range summary.ports {
		for family := range summary.ports[scope] {
			for protocol := range summary.ports[scope][family] {
				summary.ports[scope][family][protocol] = portIntervals{{443, 443}}
			}
		}
	}
	for _, field := range compactPortFields(summary) {
		if strings.HasPrefix(field.Label, "内网") && strings.Contains(field.Label, "IPv6") && field.Value != "没有匹配的内网来源" {
			t.Fatalf("missing IPv6 source mistaken for an open private port: %+v", field)
		}
	}
}
