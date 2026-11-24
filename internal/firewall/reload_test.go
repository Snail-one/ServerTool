package firewall

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/storage"
	"snail_tool/internal/ui"
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
