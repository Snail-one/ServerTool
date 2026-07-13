package runtime

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestDockerScriptInstallCancelDoesNotDownloadOrRun(t *testing.T) {
	installer := &dockerScriptInstaller{
		tempDir:       t.TempDir(),
		commandExists: func(name string) bool { return name == "sh" },
		confirm:       func() (bool, error) { return false, nil },
		download: func(string, string) error {
			t.Fatal("download called after cancellation")
			return nil
		},
		run: func(string, ...string) error {
			t.Fatal("run called after cancellation")
			return nil
		},
	}

	if err := installer.install(); err != nil {
		t.Fatal(err)
	}
}

func TestDockerScriptInstallRunsOfficialScriptAndCleansTemporaryFile(t *testing.T) {
	var calls []string
	var scriptPath string
	installer := &dockerScriptInstaller{
		tempDir: t.TempDir(),
		commandExists: func(name string) bool {
			return name == "sh" || name == "systemctl"
		},
		confirm: func() (bool, error) { return true, nil },
		download: func(url, path string) error {
			if url != dockerInstallScriptURL {
				t.Fatalf("download URL = %q", url)
			}
			scriptPath = path
			return os.WriteFile(path, []byte("#!/bin/sh\necho install\n"), 0600)
		},
		run: func(name string, args ...string) error {
			calls = append(calls, strings.Join(append([]string{name}, args...), " "))
			if name == "sh" {
				if _, err := os.Stat(args[0]); err != nil {
					t.Fatalf("script unavailable during execution: %v", err)
				}
			}
			return nil
		},
		output: func(name string, args ...string) (string, error) {
			calls = append(calls, strings.Join(append([]string{name}, args...), " "))
			return "Docker test version", nil
		},
	}

	if err := installer.install(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(scriptPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary script still exists or stat failed unexpectedly: %v", err)
	}
	joined := strings.Join(calls, "\n")
	for _, wanted := range []string{
		"sh " + scriptPath,
		"systemctl enable --now docker",
		"docker version",
	} {
		if !strings.Contains(joined, wanted) {
			t.Fatalf("calls do not contain %q:\n%s", wanted, joined)
		}
	}
}

func TestDockerScriptInstallRejectsUnexpectedContent(t *testing.T) {
	runCalled := false
	installer := &dockerScriptInstaller{
		tempDir:       t.TempDir(),
		commandExists: func(name string) bool { return name == "sh" },
		confirm:       func() (bool, error) { return true, nil },
		download: func(_ string, path string) error {
			return os.WriteFile(path, []byte("<html>not a script</html>"), 0600)
		},
		run: func(string, ...string) error {
			runCalled = true
			return nil
		},
	}

	err := installer.install()
	if err == nil || !strings.Contains(err.Error(), "不是预期") {
		t.Fatalf("error = %v", err)
	}
	if runCalled {
		t.Fatal("unexpected content was executed")
	}
}

func TestDockerScriptInstallRequiresShell(t *testing.T) {
	installer := &dockerScriptInstaller{
		commandExists: func(string) bool { return false },
	}
	err := installer.install()
	if err == nil || !strings.Contains(err.Error(), "未找到 sh") {
		t.Fatalf("error = %v", err)
	}
}
