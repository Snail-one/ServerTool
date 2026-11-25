package firewall

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/firewall/loadedrules"
	"snail_tool/internal/storage"
	"snail_tool/internal/ui"
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
	equal, err := configuration.EquivalentRules(expectedRules, loadedRules)
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
		if equal, err := configuration.EquivalentRules(expectedRules, changed); err != nil || equal {
			t.Fatalf("configuration change not detected: equal=%v err=%v", equal, err)
		}
	}
	// Reversing the verdict sequence in a chain changes firewall behaviour.
	wanted := `{"nftables":[{"table":{"family":"inet","name":"snail"}},
	 {"rule":{"family":"inet","table":"snail","chain":"input","expr":[{"accept":null}]}},
	 {"rule":{"family":"inet","table":"snail","chain":"input","expr":[{"drop":null}]}}]}`
	actual := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(wanted, "accept", "temporary"), "drop", "accept"), "temporary", "drop")
	if equal, err := configuration.EquivalentRules(wanted, actual); err != nil || equal {
		t.Fatalf("changed rule order must be unsynchronized: equal=%v err=%v", equal, err)
	}
	for _, invalid := range []string{`invalid`, `{}`, `{"nftables":[]}`} {
		if _, err := configuration.EquivalentRules(invalid, loadedRules); err == nil {
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
				if name != "unshare" || !reflect.DeepEqual(args, []string{"--net", "--", "sh", "-c", configuration.RenderConfigScript, "snail-nft-compare", configPath}) {
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
			live := loadedrules.Parse(loadedRules)
			if scenario == "changed rules" {
				live = loadedrules.Parse(strings.Replace(loadedRules, "443,22,80", "443,2222,80", 1))
			} else if scenario == "missing table" {
				live = loadedrules.Parse(`{"nftables":[]}`)
			}
			field := configStore().SyncField(content, nil, true, live.JSON, live.Err, commandExists)
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

func TestReloadSkipsSyncedRulesAndConfirmsChangesOrUnknownState(t *testing.T) {
	for _, scenario := range []struct {
		name, input, message string
		loads                int
		wantErr              bool
	}{
		{"synced", "", "配置已同步，无需加载", 0, false},
		{"different rules", "y\n", "存在差异", 1, false},
		{"different rules declined", "n\n", "存在差异", 0, false},
		{"no unshare", "y\n", "无法比较", 1, false},
		{"namespace denied", "y\n", "无法比较", 1, false},
		{"namespace denied declined", "n\n", "无法比较", 0, false},
		{"kernel query failed", "n\n", "无法比较", 0, false},
		{"file changed", "", "", 0, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			setupTest(t)
			const original = "# saved configuration; whitespace and comments do not change rules\n"
			if err := os.WriteFile(configPath, []byte(original), 0644); err != nil {
				t.Fatal(err)
			}
			commandExists = func(name string) bool {
				return name == "nft" || name == "unshare" && scenario.name != "no unshare"
			}
			loads, checks := 0, 0
			commandOutput = func(name string, args ...string) (string, error) {
				if name == "unshare" {
					want := []string{"--net", "--", "sh", "-c", configuration.RenderConfigScript, "snail-nft-compare", configPath}
					if !reflect.DeepEqual(args, want) {
						t.Fatalf("comparison must stay isolated: %v", args)
					}
					if strings.HasPrefix(scenario.name, "namespace denied") {
						return "Operation not permitted", errors.New("unshare denied")
					}
					if scenario.name == "file changed" {
						if err := os.WriteFile(configPath, []byte("# external edit\n"), 0644); err != nil {
							t.Fatal(err)
						}
					}
					return expectedRules, nil
				}
				if name != "nft" {
					t.Fatalf("unexpected command: %s %v", name, args)
				}
				switch {
				case reflect.DeepEqual(args, []string{"-c", "-f", configPath}):
					checks++
					return "", nil
				case reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}):
					if scenario.name == "kernel query failed" {
						return "permission denied", errors.New("query failed")
					}
					if strings.HasPrefix(scenario.name, "different rules") {
						return strings.Replace(loadedRules, "443,22,80", "443,2222,80", 1), nil
					}
					return loadedRules, nil
				case reflect.DeepEqual(args, []string{"-f", configPath}):
					loads++
					return "", nil
				default:
					t.Fatalf("unexpected nft call: %v", args)
					return "", nil
				}
			}
			var loadErr error
			output := capturePortOutput(t, func() { loadErr = loadConfig(inputUI(t, scenario.input)) })
			cancelled := scenario.input == "n\n"
			if cancelled && !errors.Is(loadErr, ui.ErrReturnToMenu) || !cancelled && (loadErr != nil) != scenario.wantErr || loads != scenario.loads || checks != 1 {
				t.Fatalf("err=%v checks=%d loads=%d, wantErr=%v wantLoads=%d", loadErr, checks, loads, scenario.wantErr, scenario.loads)
			}
			if !strings.Contains(output, scenario.message) {
				t.Fatalf("missing state %q: %s", scenario.message, output)
			}
			if (scenario.name == "synced" || scenario.wantErr) && strings.Contains(output, "是否立即加载") {
				t.Fatal("synced or concurrently modified config should not prompt for reload")
			}
		})
	}
}

func TestEditorWithoutContentChangesDoesNotBackUpOrLoad(t *testing.T) {
	for _, exists := range []bool{true, false} {
		t.Run(map[bool]string{true: "existing unchanged", false: "new file not saved"}[exists], func(t *testing.T) {
			setupTest(t)
			if exists {
				if err := os.WriteFile(configPath, []byte("table inet snail {}\n"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			var existingBackups []string
			if exists {
				for i := 0; i < storage.MaxBackupSnapshots; i++ {
					if _, err := configStore().Backup(); err != nil {
						t.Fatal(err)
					}
				}
				var err error
				existingBackups, err = filepath.Glob(filepath.Join(firewallBackupDir(), "nftables.conf.bak.*"))
				if err != nil || len(existingBackups) != storage.MaxBackupSnapshots {
					t.Fatalf("failed to seed backup history: %v %v", existingBackups, err)
				}
			}
			commandExists = func(name string) bool { return name == "vim" }
			commandRun = func(name string, args ...string) error {
				if name != "vim" {
					t.Fatalf("unexpected editor: %s", name)
				}
				draft := firewallEditPath(t, args)
				return os.WriteFile(draft, []byte(storage.ReadFileString(draft)), 0600)
			}
			output := capturePortOutput(t, func() {
				for i := 0; i < storage.MaxBackupSnapshots; i++ {
					if err := editConfig(); err != nil {
						t.Fatal(err)
					}
				}
			})
			backups, err := filepath.Glob(filepath.Join(firewallBackupDir(), "nftables.conf.bak.*"))
			if err != nil || !reflect.DeepEqual(backups, existingBackups) {
				t.Fatalf("unchanged edit changed backup history: %v %v", backups, err)
			}
			if !strings.Contains(output, "配置内容未修改，无需备份或加载") || strings.Contains(output, "是否立即加载") {
				t.Fatalf("unchanged edit triggered reload: %s", output)
			}
		})
	}
}
