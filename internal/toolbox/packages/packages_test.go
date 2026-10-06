package packages

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/ui"
)

func TestDetectPackageManager(t *testing.T) {
	for _, test := range []packageManager{
		{name: "apt-get", refreshArgs: []string{"update"}, installArgs: []string{"install", "-y"}},
		{name: "dnf", installArgs: []string{"install", "-y"}},
		{name: "pacman", installArgs: []string{"-Sy", "--noconfirm", "--needed"}},
		{name: "zypper", installArgs: []string{"--non-interactive", "install"}},
		{name: "apk", installArgs: []string{"add"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			restoreDependencies(t)
			commandExists = func(name string) bool { return name == test.name }
			manager, err := detectPackageManager()
			if err != nil || !reflect.DeepEqual(manager, test) {
				t.Fatalf("manager=%#v err=%v, want %#v", manager, err, test)
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
	manager := packageManager{name: "apt-get", refreshArgs: []string{"update"}, installArgs: []string{"install", "-y"}}
	if err := installPackages(manager, []commandLineTool{toolByName(t, "ripgrep"), toolByName(t, "jq")}); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"apt-get", "update"}, {"apt-get", "install", "-y", "ripgrep", "jq"}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls=%v, want %v", calls, want)
	}
}

func TestInstallSelectedSkipsInstalledPackageWithoutCommand(t *testing.T) {
	for _, name := range []string{"ripgrep", "build-essential", "bash-completion"} {
		t.Run(name, func(t *testing.T) {
			restoreDependencies(t)
			commandExists = func(name string) bool { return name == "apt-get" }
			commandOutput = func(string, ...string) (string, error) {
				return name + "\tinstall ok installed\n", nil
			}
			commandRun = func(string, ...string) error {
				t.Fatal("installed package should not invoke installation")
				return nil
			}
			if err := NewInventory().installSelected(ui.New(), toolByName(t, name)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestInstallMissingWithAptUsesSnapshotAndRefreshes(t *testing.T) {
	restoreDependencies(t)
	installed := map[string]bool{"curl": true}
	queries := 0
	commandExists = func(name string) bool { return name == "apt-get" }
	commandOutput = func(string, ...string) (string, error) {
		queries++
		var output strings.Builder
		for name, present := range installed {
			if present {
				fmt.Fprintf(&output, "%s\tinstall ok installed\n", name)
			}
		}
		return output.String(), nil
	}
	isRoot = func() bool { return true }
	commandRun = func(name string, args ...string) error {
		if args[0] == "install" {
			for _, packageName := range args[2:] {
				if packageName == "curl" {
					t.Fatal("batch install included an already installed package")
				}
				installed[packageName] = true
			}
		}
		return nil
	}
	inventory := NewInventory()
	if err := inventory.installMissing(newUIWithInput(t, "y\n")); err != nil {
		t.Fatal(err)
	}
	count, total, err := inventory.InstalledCount()
	if err != nil || count != total || queries != 2 {
		t.Fatalf("count=%d/%d queries=%d err=%v", count, total, queries, err)
	}
	if !installed["build-essential"] || !installed["bash-completion"] {
		t.Fatal("batch install omitted the additional tools")
	}
}

func TestBuildEssentialPackagesForManagers(t *testing.T) {
	tool := toolByName(t, "build-essential")
	for manager, expected := range map[string][]string{
		"apt-get": {"build-essential"}, "apt": {"build-essential"},
		"dnf": {"gcc", "gcc-c++", "make"}, "yum": {"gcc", "gcc-c++", "make"},
		"pacman": {"base-devel"}, "zypper": {"gcc", "gcc-c++", "make"}, "apk": {"build-base"},
	} {
		t.Run(manager, func(t *testing.T) {
			restoreDependencies(t)
			var calls [][]string
			commandRun = func(name string, args ...string) error {
				calls = append(calls, append([]string{name}, args...))
				return nil
			}
			if err := installPackages(packageManager{name: manager, installArgs: []string{"install"}}, []commandLineTool{tool, tool}); err != nil {
				t.Fatal(err)
			}
			want := [][]string{append([]string{manager, "install"}, expected...)}
			if !reflect.DeepEqual(calls, want) {
				t.Fatalf("calls=%v, want %v", calls, want)
			}
		})
	}
}

func TestInstallSelectedQueriesPackageAfterInstall(t *testing.T) {
	for _, name := range []string{"curl", "build-essential", "bash-completion"} {
		t.Run(name, func(t *testing.T) {
			restoreDependencies(t)
			commandExists = func(string) bool { return true }
			installed := false
			commandOutput = func(string, ...string) (string, error) {
				if installed {
					return name + "\tinstall ok installed\n", nil
				}
				return "", nil
			}
			isRoot = func() bool { return true }
			var calls [][]string
			commandRun = func(command string, args ...string) error {
				calls = append(calls, append([]string{command}, args...))
				if args[0] == "install" {
					installed = true
				}
				return nil
			}
			inventory := NewInventory()
			tool := toolByName(t, name)
			if err := inventory.installSelected(newUIWithInput(t, "y\n"), tool); err != nil {
				t.Fatal(err)
			}
			want := [][]string{{"apt-get", "update"}, {"apt-get", "install", "-y", name}}
			if !reflect.DeepEqual(calls, want) || !inventory.toolInstalled(tool) {
				t.Fatalf("install did not update package status: calls=%v", calls)
			}
		})
	}
}

func TestInstallMissingRequiresRoot(t *testing.T) {
	restoreDependencies(t)
	commandExists = func(name string) bool { return name == "apk" }
	commandOutput = func(string, ...string) (string, error) { return "", nil }
	isRoot = func() bool { return false }
	if err := NewInventory().installMissing(ui.New()); err == nil {
		t.Fatal("non-root install should fail")
	}
}

func TestInstallVerificationFailureIsNotSuccess(t *testing.T) {
	for _, queryFailure := range []bool{false, true} {
		t.Run(fmt.Sprint(queryFailure), func(t *testing.T) {
			restoreDependencies(t)
			commandExists = func(name string) bool { return name == "apt-get" }
			queries := 0
			commandOutput = func(string, ...string) (string, error) {
				queries++
				if queries > 1 && queryFailure {
					return "", errors.New("database unavailable")
				}
				return "", nil
			}
			commandRun = func(string, ...string) error { return nil }
			isRoot = func() bool { return true }
			inventory := NewInventory()
			err := inventory.installSelected(newUIWithInput(t, "y\n"), toolByName(t, "curl"))
			if err == nil || queries != 2 {
				t.Fatalf("verification failed to reject installation: queries=%d err=%v", queries, err)
			}
			if queryFailure && !strings.Contains(err.Error(), "检测失败") {
				t.Fatalf("query failure was mistaken for missing package: %v", err)
			}
		})
	}
}

func restoreDependencies(t *testing.T) {
	t.Helper()
	previousExists, previousRun, previousOutput, previousRoot := commandExists, commandRun, commandOutput, isRoot
	t.Cleanup(func() {
		commandExists, commandRun, commandOutput, isRoot = previousExists, previousRun, previousOutput, previousRoot
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
