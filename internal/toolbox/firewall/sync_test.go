package firewall

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

const expectedRules = `{"nftables":[
 {"metainfo":{"version":"1.1.3"}},
 {"table":{"family":"inet","name":"snail","handle":1}},
 {"chain":{"family":"inet","table":"snail","name":"input","hook":"input","prio":0,"policy":"drop","handle":2}},
 {"chain":{"family":"inet","table":"snail","name":"other","handle":3}},
 {"rule":{"family":"inet","table":"snail","chain":"input","handle":4,"expr":[
  {"match":{"op":"==","left":{"payload":{"protocol":"tcp","field":"dport"}},"right":{"set":[22,80,443]}}},
  {"counter":null},{"accept":null}]}},
 {"rule":{"family":"inet","table":"snail","chain":"input","handle":5,"expr":[{"drop":null}]}}
]}`

const loadedRules = `{"nftables":[
 {"metainfo":{"version":"1.1.3"}},
 {"table":{"family":"ip","name":"docker","handle":1}},
 {"table":{"family":"inet","name":"snail","handle":22}},
 {"chain":{"family":"inet","table":"snail","name":"other","handle":24}},
 {"chain":{"family":"inet","table":"snail","name":"input","hook":"input","prio":0,"policy":"drop","handle":23}},
 {"rule":{"family":"ip","table":"docker","chain":"forward","expr":[{"accept":null}]}},
 {"rule":{"family":"inet","table":"snail","chain":"input","handle":25,"expr":[
  {"match":{"op":"==","left":{"payload":{"protocol":"tcp","field":"dport"}},"right":{"set":[443,22,80]}}},
  {"counter":{"packets":500,"bytes":1048576}},{"accept":null}]}},
 {"rule":{"family":"inet","table":"snail","chain":"input","handle":26,"expr":[{"drop":null}]}}
]}`

func TestEquivalentRulesIgnoresCountersHandlesAndIndependentTables(t *testing.T) {
	equal, err := equivalentRules(expectedRules, loadedRules)
	if err != nil || !equal {
		t.Fatalf("equivalent rules reported as changed: equal=%v err=%v", equal, err)
	}
	for _, changed := range []string{
		strings.Replace(loadedRules, "443,22,80", "443,2222,80", 1),
		strings.Replace(loadedRules, `"protocol":"tcp"`, `"protocol":"udp"`, 1),
		strings.Replace(loadedRules, `"policy":"drop"`, `"policy":"accept"`, 1),
		strings.Replace(loadedRules, `"prio":0`, `"prio":10`, 1),
		strings.Replace(loadedRules, `"name":"other"`, `"name":"extra"`, 1),
		strings.Replace(loadedRules, `"snail"`, `"not-snail"`, -1),
	} {
		if equal, err := equivalentRules(expectedRules, changed); err != nil || equal {
			t.Fatalf("configuration change not detected: equal=%v err=%v", equal, err)
		}
	}
	// Reversing the verdict sequence in a chain changes firewall behaviour.
	wanted := `{"nftables":[{"table":{"family":"inet","name":"snail"}},
	 {"rule":{"family":"inet","table":"snail","chain":"input","expr":[{"accept":null}]}},
	 {"rule":{"family":"inet","table":"snail","chain":"input","expr":[{"drop":null}]}}]}`
	actual := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(wanted, "accept", "temporary"), "drop", "accept"), "temporary", "drop")
	if equal, err := equivalentRules(wanted, actual); err != nil || equal {
		t.Fatalf("changed rule order must be unsynchronized: equal=%v err=%v", equal, err)
	}
	for _, invalid := range []string{`invalid`, `{}`, `{"nftables":[]}`} {
		if _, err := equivalentRules(invalid, loadedRules); err == nil {
			t.Fatalf("invalid or ambiguous expected rules accepted: %s", invalid)
		}
	}
}

func TestConfigSyncParsesOnlyInAnIsolatedNetworkNamespace(t *testing.T) {
	for _, scenario := range []string{"synced", "changed rules", "missing table", "namespace denied", "concurrent edit", "no unshare"} {
		t.Run(scenario, func(t *testing.T) {
			setupTest(t)
			content := []byte("# configuration with include and symbolic definitions\n")
			if err := os.WriteFile(configPath, content, 0644); err != nil {
				t.Fatal(err)
			}
			commandExists = func(name string) bool { return name == "nft" || name == "unshare" && scenario != "no unshare" }
			calls := 0
			commandOutput = func(name string, args ...string) (string, error) {
				calls++
				if name != "unshare" || !reflect.DeepEqual(args, []string{"--net", "--", "sh", "-c", renderConfigScript, "snail-nft-compare", configPath}) {
					t.Fatalf("comparison must never load a config in the host namespace: %s %v", name, args)
				}
				if scenario == "namespace denied" {
					return "unshare failed: Operation not permitted", errors.New("exit status 1")
				}
				if scenario == "concurrent edit" {
					if err := os.WriteFile(configPath, []byte("# modified\n"), 0644); err != nil {
						t.Fatal(err)
					}
				}
				return expectedRules, nil
			}
			live := parseLiveRules(loadedRules)
			if scenario == "changed rules" {
				live = parseLiveRules(strings.Replace(loadedRules, "443,22,80", "443,2222,80", 1))
			} else if scenario == "missing table" {
				live = parseLiveRules(`{"nftables":[]}`)
			}
			field := configSyncField(content, nil, true, live)
			want := "无法比较"
			if scenario == "synced" {
				want = "已同步"
			} else if scenario == "changed rules" || scenario == "missing table" {
				want = "未同步"
			}
			if !strings.HasPrefix(field.Value, want) {
				t.Fatalf("sync state=%q detail=%q, want %q", field.Value, field.Detail, want)
			}
			wantCalls := 1
			if scenario == "no unshare" {
				wantCalls = 0
			}
			if calls != wantCalls {
				t.Fatalf("queries=%d, want %d; comparison must never fall back to the host", calls, wantCalls)
			}
		})
	}
}
