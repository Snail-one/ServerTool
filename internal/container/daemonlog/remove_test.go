package daemonlog

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/shared"
)

type logRemovalConfirmFunc func(string) (bool, error)

func (f logRemovalConfirmFunc) Confirm(prompt string) (bool, error) { return f(prompt) }

func managedLogConfig(t *testing.T, original string) string {
	t.Helper()
	path := writeDaemonJSONFile(t, original)
	if err := backupStore.RememberManaged(path, []byte(original)); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRemoveLogRotationBacksUpAndPreservesOtherDaemonSettings(t *testing.T) {
	original := `{"debug":true,"number":9007199254740993,"registry-mirrors":["https://mirror.example"],"proxies":{"http-proxy":"http://proxy:8080"},"log-driver":"json-file","log-opts":{"max-size":"100m","max-file":"3"}}`
	path := managedLogConfig(t, original)
	var calls []systemRunCall
	err := remove(logRemovalConfirmFunc(func(string) (bool, error) { return true, nil }), path,
		func() bool { return true }, func(string) bool { return true },
		func(name string, args ...string) error {
			calls = append(calls, systemRunCall{name: name, args: args})
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	data := shared.ReadFileString(path)
	for _, lost := range []string{"log-driver", "log-opts"} {
		if strings.Contains(data, lost) {
			t.Fatalf("log configuration remains: %s", data)
		}
	}
	for _, kept := range []string{`"debug": true`, "9007199254740993", "https://mirror.example", "http://proxy:8080"} {
		if !strings.Contains(data, kept) {
			t.Fatalf("unrelated setting lost: %s", data)
		}
	}
	if !reflect.DeepEqual(calls, []systemRunCall{{name: "systemctl", args: []string{"try-restart", "docker.service"}}}) {
		t.Fatal(calls)
	}
	entries, err := backupStore.List()
	if err != nil || len(entries) != 1 || shared.ReadFileString(entries[0].Path) != original {
		t.Fatalf("original backup missing: %v, %v", entries, err)
	}
	if matches, err := backupStore.MatchesManaged(path, []byte(original)); err != nil || matches {
		t.Fatalf("managed record remains: %v, %v", matches, err)
	}
	if got := summarizeLogConfig(path); got != "未配置" {
		t.Fatalf("status not updated: %s", got)
	}
}

func TestRemoveLogRotationCancellationConcurrentChangesAndBackupFailure(t *testing.T) {
	for _, scenario := range []string{"cancel", "return", "concurrent edit", "backup failure"} {
		t.Run(scenario, func(t *testing.T) {
			original := `{"log-driver":"json-file","log-opts":{"max-size":"100m"}}`
			path := managedLogConfig(t, original)
			store := backupStore
			want := original
			prompt := logRemovalConfirmFunc(func(string) (bool, error) {
				switch scenario {
				case "cancel":
					return false, nil
				case "return":
					return false, shared.ErrReturnToMenu
				case "concurrent edit":
					want += "\n"
					if err := os.WriteFile(path, []byte(want), 0600); err != nil {
						t.Fatal(err)
					}
				case "backup failure":
					backupStore.Root = path
				}
				return true, nil
			})
			err := remove(prompt, path, func() bool { return true }, func(string) bool { return true }, func(string, ...string) error {
				t.Fatal("unexpected restart on refused removal")
				return nil
			})
			if err == nil || shared.ReadFileString(path) != want {
				t.Fatalf("refused removal changed config: %v", err)
			}
			if (scenario == "cancel" || scenario == "return") && !errors.Is(err, shared.ErrReturnToMenu) {
				t.Fatalf("cancellation did not return to menu: %v", err)
			}
			if entries, err := store.List(); err != nil || len(entries) != 0 {
				t.Fatalf("refused removal created backup: %v, %v", entries, err)
			}
		})
	}
}

func TestPrepareLogRemovalRefusesInvalidUnownedAndChangedFiles(t *testing.T) {
	for _, original := range []string{"null", "[]", "{}{}", "invalid", `{"log-driver":"json-file"}`} {
		t.Run(original, func(t *testing.T) {
			path := writeDaemonJSONFile(t, original)
			if plan, err := PrepareRemoval(path, backupStore); err == nil || plan != nil {
				t.Fatalf("invalid or unowned config accepted: %v, %v", plan, err)
			}
			if err := backupStore.RememberManaged(path, []byte(original+"\n")); err != nil {
				t.Fatal(err)
			}
			if plan, err := PrepareRemoval(path, backupStore); err == nil || plan != nil || shared.ReadFileString(path) != original {
				t.Fatalf("modified config accepted: %v, %v", plan, err)
			}
		})
	}
}

func TestRemoveLogRotationNoopDoesNotPromptOrRestart(t *testing.T) {
	setupBackupStore(t)
	path := filepath.Join(t.TempDir(), "daemon.json")
	for _, exists := range []bool{false, true} {
		if exists {
			if err := os.WriteFile(path, []byte(`{"debug":true}`), 0600); err != nil {
				t.Fatal(err)
			}
		}
		err := remove(logRemovalConfirmFunc(func(string) (bool, error) { t.Fatal("unexpected confirmation"); return false, nil }), path,
			func() bool { return true }, func(string) bool { return true },
			func(string, ...string) error { t.Fatal("unexpected restart"); return nil })
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestRemoveLogRotationReportsRestartFailureAndHandlesAbsentService(t *testing.T) {
	for _, hasService := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent service", true: "restart failure"}[hasService], func(t *testing.T) {
			path := managedLogConfig(t, `{"log-driver":"json-file","log-opts":{"max-size":"100m"}}`)
			calls := 0
			err := remove(logRemovalConfirmFunc(func(string) (bool, error) { return true, nil }), path,
				func() bool { return true }, func(string) bool { return hasService },
				func(string, ...string) error { calls++; return errors.New("systemctl failed") })
			if hasService {
				if err == nil || !strings.Contains(err.Error(), "已移除") || calls != 1 {
					t.Fatalf("missing partial success report: %d calls, %v", calls, err)
				}
			} else if err != nil || calls != 0 {
				t.Fatalf("absent service was started: %d calls, %v", calls, err)
			}
			if data := shared.ReadFileString(path); data != "{}\n" {
				t.Fatalf("log-only config was not emptied: %s", data)
			}
		})
	}
}
