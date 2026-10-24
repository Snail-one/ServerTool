package daemonproxy

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/shared"
)

type confirmFunc func(string) (bool, error)

func (f confirmFunc) Confirm(prompt string) (bool, error) { return f(prompt) }

func removalSetup(t *testing.T, content string) string {
	t.Helper()
	previous := managedStore
	managedStore = shared.BackupStore{Root: filepath.Join(t.TempDir(), "data"), Module: "docker"}
	t.Cleanup(func() { managedStore = previous })
	path := filepath.Join(t.TempDir(), "http-proxy.conf")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := managedStore.RememberManaged(path, []byte(content)); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRemoveServiceProxyPreservesOtherSettingsAndBacksUp(t *testing.T) {
	original := buildDockerProxyConfig("http://proxy:8080/") + "LimitNOFILE=65536\nEnvironment=\"OTHER=keep\"\n"
	path := removalSetup(t, original)
	var calls []string
	err := remove(confirmFunc(func(string) (bool, error) { return true, nil }), path,
		func() bool { return true }, func(string) bool { return true },
		func(name string, args ...string) error {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if got := shared.ReadFileString(path); got != "[Service]\nLimitNOFILE=65536\nEnvironment=\"OTHER=keep\"\n" {
		t.Fatalf("other systemd settings changed: %s", got)
	}
	if !reflect.DeepEqual(calls, []string{"systemctl daemon-reload", "systemctl try-restart docker.service"}) {
		t.Fatal(calls)
	}
	entries, err := managedStore.List()
	if err != nil || len(entries) != 1 || shared.ReadFileString(entries[0].Path) != original {
		t.Fatalf("missing original backup: %v, %v", entries, err)
	}
	plan, err := PrepareRemoval(path, managedStore)
	if err != nil || plan != nil {
		t.Fatalf("removed proxy remains: %v, %v", plan, err)
	}
}

func TestRemoveServiceProxyCancellationAndConcurrentChanges(t *testing.T) {
	for _, scenario := range []string{"cancel", "return", "concurrent edit", "backup failure"} {
		t.Run(scenario, func(t *testing.T) {
			original := buildDockerProxyConfig("http://proxy:8080/")
			path := removalSetup(t, original)
			want := original
			prompt := confirmFunc(func(string) (bool, error) {
				switch scenario {
				case "cancel":
					return false, nil
				case "return":
					return false, shared.ErrReturnToMenu
				case "concurrent edit":
					want += "LimitNOFILE=65536\n"
					if err := os.WriteFile(path, []byte(want), 0600); err != nil {
						t.Fatal(err)
					}
				case "backup failure":
					managedStore.Root = path
				}
				return true, nil
			})
			err := remove(prompt, path, func() bool { return true }, func(string) bool { return true }, func(string, ...string) error {
				t.Fatal("systemctl called on refused removal")
				return nil
			})
			if err == nil || shared.ReadFileString(path) != want {
				t.Fatalf("refused removal changed file: %v", err)
			}
			if (scenario == "cancel" || scenario == "return") && !errors.Is(err, shared.ErrReturnToMenu) {
				t.Fatalf("cancellation did not return to menu: %v", err)
			}
		})
	}
}

func TestRemoveServiceProxyReportsApplyFailureAndHandlesAbsentService(t *testing.T) {
	for _, scenario := range []string{"reload failure", "restart failure", "absent service"} {
		t.Run(scenario, func(t *testing.T) {
			path := removalSetup(t, buildDockerProxyConfig("http://proxy:8080/"))
			var calls []string
			err := remove(confirmFunc(func(string) (bool, error) { return true, nil }), path,
				func() bool { return true }, func(string) bool { return scenario != "absent service" },
				func(_ string, args ...string) error {
					calls = append(calls, strings.Join(args, " "))
					if scenario == "reload failure" || scenario == "restart failure" && args[0] == "try-restart" {
						return errors.New("systemctl failed")
					}
					return nil
				})
			if scenario == "absent service" {
				if err != nil || !reflect.DeepEqual(calls, []string{"daemon-reload"}) {
					t.Fatalf("unexpected service start: %v, %v", calls, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "已移除") {
				t.Fatalf("missing partial success report: %v", err)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("proxy-only drop-in remains: %v", err)
			}
			if ok, err := managedStore.MatchesManaged(path, []byte(buildDockerProxyConfig("http://proxy:8080/"))); err != nil || ok {
				t.Fatalf("managed record remains: %v, %v", ok, err)
			}
		})
	}
}

func TestRemoveServiceProxyNoopDoesNotPromptOrRunCommands(t *testing.T) {
	path := removalSetup(t, "[Service]\nLimitNOFILE=65536\n")
	prompt := confirmFunc(func(string) (bool, error) { t.Fatal("unexpected confirmation"); return false, nil })
	run := func(string, ...string) error { t.Fatal("unexpected systemctl command"); return nil }
	for _, path := range []string{path, filepath.Join(t.TempDir(), "missing.conf")} {
		if err := remove(prompt, path, func() bool { return true }, func(string) bool { return true }, run); err != nil {
			t.Fatal(err)
		}
	}
}
