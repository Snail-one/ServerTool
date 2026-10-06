package packages

import (
	"errors"
	"os"
	"reflect"
	"strings"
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
	err := installPackages(manager, []commandLineTool{toolByName(t, "ripgrep"), toolByName(t, "jq")})
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

	if err := installSelected(ui.New(), toolByName(t, "ripgrep")); err != nil {
		t.Fatal(err)
	}
}

func TestInstallMissingWithApt(t *testing.T) {
	restoreDependencies(t)
	installed := map[string]bool{"apt-get": true, "dpkg-query": true}
	commandExists = func(name string) bool { return installed[name] }
	completionInstalled = func() bool { return installed["bash-completion"] }
	commandOutput = func(string, ...string) (string, error) {
		if installed["build-essential"] {
			return "install ok installed", nil
		}
		return "", errors.New("package not installed")
	}
	isRoot = func() bool { return true }
	commandRun = func(name string, args ...string) error {
		if name == "apt-get" && len(args) > 0 && args[0] == "install" {
			if !strings.Contains(strings.Join(args, " "), "build-essential") {
				t.Fatal("batch install did not include build-essential")
			}
			if !strings.Contains(strings.Join(args, " "), "bash-completion") {
				t.Fatal("batch install did not include bash-completion")
			}
			installed["build-essential"] = true
			for _, tool := range commonTools {
				installed[tool.packageName] = true
				installed[tool.command] = true
				for _, command := range tool.extraCommands {
					installed[command] = true
				}
			}
		}
		return nil
	}

	if err := installMissing(newUIWithInput(t, "y\n")); err != nil {
		t.Fatal(err)
	}
	for _, tool := range commonTools {
		if !tool.installed() {
			t.Fatalf("%s was not installed", tool.name)
		}
	}
}

func TestBuildEssentialInstallationStatus(t *testing.T) {
	tool := toolByName(t, "build-essential")
	tests := []struct {
		name          string
		commands      map[string]bool
		packageStatus string
		want          bool
	}{
		{name: "gcc alone", commands: map[string]bool{"gcc": true}},
		{name: "missing make", commands: map[string]bool{"gcc": true, "g++": true}},
		{name: "non Debian toolchain", commands: map[string]bool{"gcc": true, "g++": true, "make": true}, want: true},
		{name: "Debian package absent", commands: map[string]bool{"gcc": true, "g++": true, "make": true, "apt-get": true, "dpkg-query": true}},
		{name: "Debian package removed", commands: map[string]bool{"gcc": true, "g++": true, "make": true, "apt": true, "dpkg-query": true}, packageStatus: "deinstall ok config-files"},
		{name: "Debian package installed", commands: map[string]bool{"gcc": true, "g++": true, "make": true, "apt-get": true, "dpkg-query": true}, packageStatus: "install ok installed\n", want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			restoreDependencies(t)
			commandExists = func(name string) bool { return test.commands[name] }
			commandOutput = func(name string, args ...string) (string, error) {
				if name != "dpkg-query" || !reflect.DeepEqual(args, []string{"-W", "-f=${Status}", "build-essential"}) {
					t.Fatalf("unexpected package query: %s %v", name, args)
				}
				return test.packageStatus, nil
			}
			if got := tool.installed(); got != test.want {
				t.Fatalf("installed = %v, want %v", got, test.want)
			}
		})
	}
}

func TestBuildEssentialPackagesForManagers(t *testing.T) {
	tool := toolByName(t, "build-essential")
	for manager, expected := range map[string][]string{
		"apt-get": {"build-essential"},
		"apt":     {"build-essential"},
		"dnf":     {"gcc", "gcc-c++", "make"},
		"yum":     {"gcc", "gcc-c++", "make"},
		"pacman":  {"base-devel"},
		"zypper":  {"gcc", "gcc-c++", "make"},
		"apk":     {"build-base"},
	} {
		t.Run(manager, func(t *testing.T) {
			restoreDependencies(t)
			var calls [][]string
			commandRun = func(name string, args ...string) error {
				calls = append(calls, append([]string{name}, args...))
				return nil
			}
			// Repeating the same tool must not duplicate packages in the command.
			if err := installPackages(packageManager{name: manager, installArgs: []string{"install"}}, []commandLineTool{tool, tool}); err != nil {
				t.Fatal(err)
			}
			want := [][]string{append([]string{manager, "install"}, expected...)}
			if !reflect.DeepEqual(calls, want) {
				t.Fatalf("install calls = %v, want %v", calls, want)
			}
		})
	}
}

func TestInstallBuildEssentialWithApt(t *testing.T) {
	restoreDependencies(t)
	tool := toolByName(t, "build-essential")
	installed := map[string]bool{"apt-get": true, "dpkg-query": true, "gcc": true}
	commandExists = func(name string) bool { return installed[name] }
	commandOutput = func(string, ...string) (string, error) {
		if installed["build-essential"] {
			return "install ok installed", nil
		}
		return "", errors.New("package not installed")
	}
	isRoot = func() bool { return true }
	var calls [][]string
	commandRun = func(name string, args ...string) error {
		calls = append(calls, append([]string{name}, args...))
		if args[0] == "install" {
			for _, command := range []string{"gcc", "g++", "make", "build-essential"} {
				installed[command] = true
			}
		}
		return nil
	}
	if err := installSelected(newUIWithInput(t, "y\n"), tool); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"apt-get", "update"}, {"apt-get", "install", "-y", "build-essential"}}
	if !reflect.DeepEqual(calls, want) || !tool.installed() {
		t.Fatalf("incomplete toolchain was not installed correctly: calls=%v", calls)
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
	previousOutput := commandOutput
	previousCompletion := completionInstalled
	previousRoot := isRoot
	t.Cleanup(func() {
		commandExists = previousExists
		commandRun = previousRun
		commandOutput = previousOutput
		completionInstalled = previousCompletion
		isRoot = previousRoot
	})
}

func toolByName(t *testing.T, name string) commandLineTool {
	t.Helper()
	for _, tool := range commonTools {
		if tool.name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not found", name)
	return commandLineTool{}
}

func TestInstallBashCompletionWithApt(t *testing.T) {
	restoreDependencies(t)
	tool := toolByName(t, "bash-completion")
	installed := false
	completionInstalled = func() bool { return installed }
	commandExists = func(name string) bool { return name == "apt-get" || name == "bash" }
	isRoot = func() bool { return true }
	var calls [][]string
	commandRun = func(name string, args ...string) error {
		calls = append(calls, append([]string{name}, args...))
		if args[0] == "install" {
			installed = true
		}
		return nil
	}
	if err := installSelected(newUIWithInput(t, "y\n"), tool); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"apt-get", "update"}, {"apt-get", "install", "-y", "bash-completion"}}
	if !reflect.DeepEqual(calls, want) || !tool.installed() {
		t.Fatalf("Bash completion install failed: calls=%v", calls)
	}
}

func TestInstallBashCompletionSkipsInstalledPackage(t *testing.T) {
	restoreDependencies(t)
	completionInstalled = func() bool { return true }
	commandExists = func(string) bool { return false }
	commandRun = func(string, ...string) error {
		t.Fatal("installed Bash completion should not invoke the package manager")
		return nil
	}
	if err := installSelected(ui.New(), toolByName(t, "bash-completion")); err != nil {
		t.Fatal(err)
	}
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
