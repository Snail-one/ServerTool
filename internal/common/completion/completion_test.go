package completion

import (
	"os"
	"strings"
	"testing"

	"snail_tool/internal/ui"
)

func TestIsInstalledUsesScriptOrPackage(t *testing.T) {
	restoreCompletionDeps(t)

	fileExists = func(path string) bool { return path == bashCompletionScript }
	commandExists = func(string) bool { return false }
	if !IsInstalled() {
		t.Fatal("completion script should count as installed")
	}

	fileExists = func(string) bool { return false }
	commandExists = func(name string) bool { return name == "dpkg-query" }
	commandOutput = func(name string, args ...string) (string, error) {
		if name == "dpkg-query" && args[len(args)-1] == packageName {
			return "install ok installed", nil
		}
		return "", os.ErrNotExist
	}
	if !IsInstalled() {
		t.Fatal("installed package should count as installed")
	}

	commandOutput = func(string, ...string) (string, error) {
		return "", os.ErrNotExist
	}
	if IsInstalled() {
		t.Fatal("missing script and package should be uninstalled")
	}
}

func TestConfigureSkipsInstallWhenPresent(t *testing.T) {
	restoreCompletionDeps(t)
	fileExists = func(path string) bool { return path == bashCompletionScript }
	commandRun = func(string, ...string) error {
		t.Fatal("already installed completion should not run a package manager")
		return nil
	}

	if err := Configure(newUIWithInput(t, "")); err != nil {
		t.Fatal(err)
	}
}

func TestConfigureCancelsInstall(t *testing.T) {
	restoreCompletionDeps(t)
	fileExists = func(string) bool { return false }
	commandExists = func(string) bool { return false }
	commandRun = func(string, ...string) error {
		t.Fatal("cancelled install should not run a package manager")
		return nil
	}

	if err := Configure(newUIWithInput(t, "n\n")); err != nil {
		t.Fatal(err)
	}
}

func TestConfigureInstallsWithApt(t *testing.T) {
	restoreCompletionDeps(t)
	installed := false
	var ran []string
	fileExists = func(path string) bool { return installed && path == bashCompletionScript }
	commandExists = func(name string) bool { return name == "apt" }
	commandRun = func(name string, args ...string) error {
		ran = append(ran, name+" "+strings.Join(args, " "))
		if name == "apt" && len(args) > 0 && args[0] == "install" {
			installed = true
		}
		return nil
	}

	if err := Configure(newUIWithInput(t, "y\n")); err != nil {
		t.Fatal(err)
	}
	if !installed {
		t.Fatal("expected apt install to mark bash-completion installed")
	}
	if strings.Join(ran, "; ") != "apt update; apt install -y bash-completion" {
		t.Fatalf("unexpected install commands: %#v", ran)
	}
}

func restoreCompletionDeps(t *testing.T) {
	t.Helper()
	previousExists := commandExists
	previousOutput := commandOutput
	previousRun := commandRun
	previousFile := fileExists
	t.Cleanup(func() {
		commandExists = previousExists
		commandOutput = previousOutput
		commandRun = previousRun
		fileExists = previousFile
	})
}

func newUIWithInput(t *testing.T, input string) *ui.UI {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.WriteString(input); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	previousStdin := os.Stdin
	os.Stdin = reader
	view := ui.New()
	os.Stdin = previousStdin
	t.Cleanup(func() { _ = reader.Close() })
	return view
}
