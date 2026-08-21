package packages

import (
	"os"
	"reflect"
	"testing"

	"snail_tool/internal/ui"
)

func TestDetectPackageManager(t *testing.T) {
	tests := []struct {
		name        string
		refreshArgs []string
		installArgs []string
	}{
		{name: "apt-get", refreshArgs: []string{"update"}, installArgs: []string{"install", "-y"}},
		{name: "dnf", installArgs: []string{"install", "-y"}},
		{name: "pacman", installArgs: []string{"-Sy", "--noconfirm", "--needed"}},
		{name: "zypper", installArgs: []string{"--non-interactive", "install"}},
		{name: "apk", installArgs: []string{"add"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			restoreDependencies(t)
			commandExists = func(name string) bool { return name == test.name }

			manager, err := detectPackageManager()
			if err != nil {
				t.Fatal(err)
			}
			if manager.name != test.name ||
				!reflect.DeepEqual(manager.refreshArgs, test.refreshArgs) ||
				!reflect.DeepEqual(manager.installArgs, test.installArgs) {
				t.Fatalf("unexpected manager configuration: %#v", manager)
			}
		})
	}
}

func TestInstallPackagesRefreshesAptAndInstallsAll(t *testing.T) {
	restoreDependencies(t)
	var calls [][]string
	commandRun = func(name string, args ...string) error {
		calls = append(calls, append([]string{name}, args...))
		return nil
	}

	manager := packageManager{
		name:        "apt-get",
		refreshArgs: []string{"update"},
		installArgs: []string{"install", "-y"},
	}
	err := installPackages(manager, []commandLineTool{commonTools[0], commonTools[1]})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"apt-get", "update"},
		{"apt-get", "install", "-y", "ripgrep", "jq"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("unexpected package manager calls: %#v", calls)
	}
}

func TestInstallSelectedSkipsInstalledTool(t *testing.T) {
	restoreDependencies(t)
	commandExists = func(name string) bool { return name == "rg" }
	commandRun = func(string, ...string) error {
		t.Fatal("installed tool should not invoke package manager")
		return nil
	}

	if err := installSelected(ui.New(), commonTools[0]); err != nil {
		t.Fatal(err)
	}
}

func TestInstallMissingWithApt(t *testing.T) {
	restoreDependencies(t)
	installed := map[string]bool{"apt-get": true}
	commandExists = func(name string) bool { return installed[name] }
	isRoot = func() bool { return true }
	commandRun = func(name string, args ...string) error {
		if name == "apt-get" && len(args) > 0 && args[0] == "install" {
			for _, tool := range commonTools {
				installed[tool.command] = true
			}
		}
		return nil
	}

	if err := installMissing(newUIWithInput(t, "y\n")); err != nil {
		t.Fatal(err)
	}
	for _, tool := range commonTools {
		if !installed[tool.command] {
			t.Fatalf("%s was not installed", tool.command)
		}
	}
}

func TestInstallMissingRequiresRoot(t *testing.T) {
	restoreDependencies(t)
	commandExists = func(name string) bool { return name == "apk" }
	isRoot = func() bool { return false }

	if err := installMissing(ui.New()); err == nil {
		t.Fatal("non-root install should fail")
	}
}

func restoreDependencies(t *testing.T) {
	t.Helper()
	previousExists := commandExists
	previousRun := commandRun
	previousRoot := isRoot
	t.Cleanup(func() {
		commandExists = previousExists
		commandRun = previousRun
		isRoot = previousRoot
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
