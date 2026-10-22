package firewall

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/shared"
)

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
					want := []string{"--net", "--", "sh", "-c", renderConfigScript, "snail-nft-compare", configPath}
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
			if cancelled && !errors.Is(loadErr, shared.ErrReturnToMenu) || !cancelled && (loadErr != nil) != scenario.wantErr || loads != scenario.loads || checks != 1 {
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

func TestEditorWithoutContentChangesDoesNotValidateOrPromptForReload(t *testing.T) {
	for _, exists := range []bool{true, false} {
		t.Run(map[bool]string{true: "existing unchanged", false: "new file not saved"}[exists], func(t *testing.T) {
			setupTest(t)
			if exists {
				if err := os.WriteFile(configPath, []byte("table inet snail {}\n"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			commandExists = func(name string) bool { return name == "vim" }
			commandRun = func(name string, args ...string) error {
				if name != "vim" || !reflect.DeepEqual(args, []string{configPath}) {
					t.Fatalf("unexpected editor: %s %v", name, args)
				}
				return nil
			}
			output := capturePortOutput(t, func() {
				if err := editConfig(inputUI(t, "")); err != nil {
					t.Fatal(err)
				}
			})
			if !strings.Contains(output, "配置内容未修改，无需加载") || strings.Contains(output, "是否立即加载") {
				t.Fatalf("unchanged edit triggered reload: %s", output)
			}
		})
	}
}
