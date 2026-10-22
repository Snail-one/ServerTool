package cleanup

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/shared"
)

func setupDockerCleanup(t *testing.T, daemon, proxy string) {
	t.Helper()
	previousDaemon, previousProxy := dockerDaemonPath, dockerProxyPath
	previousRun, previousExists, previousUnit := cleanupCommandRun, cleanupCommandExists, cleanupUnitExists
	previousStore := dockerManagedStore
	dockerManagedStore = shared.BackupStore{Root: filepath.Join(t.TempDir(), "snailtool"), Module: "docker"}
	t.Cleanup(func() {
		dockerDaemonPath, dockerProxyPath = previousDaemon, previousProxy
		cleanupCommandRun, cleanupCommandExists, cleanupUnitExists = previousRun, previousExists, previousUnit
		dockerManagedStore = previousStore
	})
	dir := t.TempDir()
	dockerDaemonPath, dockerProxyPath = filepath.Join(dir, "daemon.json"), filepath.Join(dir, "http-proxy.conf")
	for path, content := range map[string]string{dockerDaemonPath: daemon, dockerProxyPath: proxy} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if err := dockerManagedStore.RememberManaged(path, []byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	cleanupCommandExists = func(string) bool { return true }
	cleanupUnitExists = func(string) bool { return true }
	cleanupCommandRun = func(name string, args ...string) error {
		t.Fatalf("unexpected command: %s %v", name, args)
		return nil
	}
}

func TestDockerCleanupPreservesUnrelatedSettingsAndRestartsOnce(t *testing.T) {
	setupDockerCleanup(t, `{"log-driver":"json-file","log-opts":{"max-size":"50m"},"debug":true,"number":9007199254740993}`, "[Service]\nEnvironment=\"HTTP_PROXY=http://example:8080\"\nEnvironment=\"HTTPS_PROXY=http://example:8080\"\nEnvironment=\"NO_PROXY=localhost\"\nLimitNOFILE=65536\nEnvironment=\"OTHER=keep\"\n")
	var calls []string
	cleanupCommandRun = func(name string, args ...string) error {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil
	}
	if err := cleanupDocker(nil); err != nil {
		t.Fatal(err)
	}
	data := shared.ReadFileString(dockerDaemonPath)
	if strings.Contains(data, "log-driver") || strings.Contains(data, "log-opts") || !strings.Contains(data, "9007199254740993") || !strings.Contains(data, `"debug": true`) {
		t.Fatalf("unrelated daemon settings changed: %s", data)
	}
	proxy := shared.ReadFileString(dockerProxyPath)
	if strings.Contains(proxy, "HTTP_PROXY") || strings.Contains(proxy, "HTTPS_PROXY") || strings.Contains(proxy, "NO_PROXY") || !strings.Contains(proxy, "LimitNOFILE=65536") || !strings.Contains(proxy, "OTHER=keep") {
		t.Fatalf("unrelated systemd settings changed: %s", proxy)
	}
	if !reflect.DeepEqual(calls, []string{"systemctl daemon-reload", "systemctl try-restart docker.service"}) {
		t.Fatal(calls)
	}
	if err := cleanupDocker(nil); err != nil || len(calls) != 2 {
		t.Fatalf("repeated cleanup must not restart again: %v %v", err, calls)
	}
}

func TestDockerCleanupDeletesProxyOnlyWhenNoOtherSettingsRemain(t *testing.T) {
	setupDockerCleanup(t, "{}", "[Service]\nEnvironment=\"HTTP_PROXY=http://example:8080\"\n")
	cleanupCommandRun = func(string, ...string) error { return nil }
	if err := cleanupDocker(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dockerProxyPath); !os.IsNotExist(err) {
		t.Fatalf("empty proxy drop-in remains: %v", err)
	}
}

func TestDockerCleanupPreflightsFilesAndReportsServiceFailure(t *testing.T) {
	for _, scenario := range []string{"invalid json", "complex proxy", "symlink", "service failure"} {
		t.Run(scenario, func(t *testing.T) {
			daemon := `{"log-driver":"json-file","keep":true}`
			proxy := "[Service]\nEnvironment=\"HTTP_PROXY=http://example:8080\"\n"
			if scenario == "invalid json" {
				daemon = "invalid"
			}
			if scenario == "complex proxy" {
				proxy = "[Service]\nEnvironment=\"HTTP_PROXY=http://example:8080\" \"OTHER=keep\"\n"
			}
			setupDockerCleanup(t, daemon, proxy)
			if scenario == "symlink" {
				if err := os.Remove(dockerProxyPath); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(dockerDaemonPath, dockerProxyPath); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "service failure" {
				cleanupCommandRun = func(string, ...string) error { return errors.New("systemctl failed") }
			}
			if err := cleanupDocker(nil); err == nil {
				t.Fatal("expected cleanup error")
			}
			if scenario != "service failure" && shared.ReadFileString(dockerDaemonPath) != daemon {
				t.Fatal("configuration changed before all files were validated")
			}
		})
	}
}

func TestDockerCleanupRefusesUnownedAndManuallyChangedFiles(t *testing.T) {
	for _, scenario := range []string{"no record", "manual daemon changes", "manual proxy changes"} {
		t.Run(scenario, func(t *testing.T) {
			setupDockerCleanup(t, `{"log-driver":"json-file","debug":true}`, "[Service]\nEnvironment=\"HTTP_PROXY=http://example:8080\"\n")
			if scenario == "no record" {
				dockerManagedStore.Root = filepath.Join(t.TempDir(), "snailtool")
			}
			changed := dockerDaemonPath
			if scenario == "manual proxy changes" {
				changed = dockerProxyPath
			}
			if scenario != "no record" {
				if err := os.WriteFile(changed, []byte(shared.ReadFileString(changed)+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			daemon, proxy := shared.ReadFileString(dockerDaemonPath), shared.ReadFileString(dockerProxyPath)
			if err := cleanupDocker(nil); err == nil {
				t.Fatal("unknown ownership accepted")
			}
			if shared.ReadFileString(dockerDaemonPath) != daemon || shared.ReadFileString(dockerProxyPath) != proxy {
				t.Fatal("ambiguous cleanup changed files")
			}
		})
	}
}
