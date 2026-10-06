package packages

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestQueryInstalledPackages(t *testing.T) {
	for _, test := range []struct {
		manager string
		command string
		args    []string
		output  string
	}{
		{"apt-get", "dpkg-query", []string{"-W", "-f=${Package}\t${Status}\n"}, "curl\tinstall ok installed\nbash-completion\thold ok installed\ntree\tdeinstall ok config-files\njq\tinstall ok unpacked\nwget\tinstall reinstreq installed\n"},
		{"apt", "dpkg-query", []string{"-W", "-f=${Package}\t${Status}\n"}, "curl:amd64\tinstall ok installed\nbash-completion\tinstall ok installed\n"},
		{"dnf", "rpm", []string{"-qa", "--queryformat", "%{NAME}\n"}, "curl\nbash-completion\n"},
		{"yum", "rpm", []string{"-qa", "--queryformat", "%{NAME}\n"}, "curl\nbash-completion\n"},
		{"zypper", "rpm", []string{"-qa", "--queryformat", "%{NAME}\n"}, "curl\nbash-completion\n"},
		{"pacman", "pacman", []string{"-Qq"}, "curl\nbash-completion\n"},
		{"apk", "apk", []string{"info"}, "curl\nbash-completion\n"},
	} {
		t.Run(test.manager, func(t *testing.T) {
			restoreDependencies(t)
			queries := 0
			commandOutput = func(name string, args ...string) (string, error) {
				queries++
				if name != test.command || !reflect.DeepEqual(args, test.args) {
					t.Fatalf("unexpected query: %s %v", name, args)
				}
				return test.output, nil
			}
			got, err := queryInstalledPackages(packageManager{name: test.manager})
			want := map[string]bool{"curl": true, "bash-completion": true}
			if err != nil || queries != 1 || !reflect.DeepEqual(got, want) {
				t.Fatalf("installed=%v queries=%d err=%v, want %v", got, queries, err, want)
			}
		})
	}
}

func TestInventorySharesSnapshotAndRefreshes(t *testing.T) {
	restoreDependencies(t)
	commandExists = func(name string) bool { return name == "apt-get" }
	queries := 0
	output := "curl\tinstall ok installed\n"
	commandOutput = func(string, ...string) (string, error) {
		queries++
		return output, nil
	}
	inventory := NewInventory()
	for i := 0; i < 3; i++ {
		count, total, err := inventory.InstalledCount()
		if count != 1 || total != 10 || err != nil {
			t.Fatalf("count=%d/%d err=%v", count, total, err)
		}
		for _, tool := range commonTools {
			_ = inventory.badge(tool)
		}
	}
	output += "bash-completion\tinstall ok installed\n"
	if inventory.toolInstalled(toolByName(t, "bash-completion")) || queries != 1 {
		t.Fatal("reading cached status unexpectedly queried the database")
	}
	if err := inventory.Refresh(); err != nil {
		t.Fatal(err)
	}
	count, _, err := inventory.InstalledCount()
	if err != nil || count != 2 || queries != 2 {
		t.Fatalf("refresh did not update shared state: count=%d queries=%d err=%v", count, queries, err)
	}
}

func TestInventoryRequiresAllMappedPackages(t *testing.T) {
	restoreDependencies(t)
	commandExists = func(name string) bool { return name == "dnf" }
	output := "gcc\nmake\n"
	commandOutput = func(string, ...string) (string, error) { return output, nil }
	inventory := NewInventory()
	tool := toolByName(t, "build-essential")
	if inventory.toolInstalled(tool) {
		t.Fatal("partial compiler packages were marked installed")
	}
	output += "gcc-c++\n"
	if err := inventory.Refresh(); err != nil || !inventory.toolInstalled(tool) {
		t.Fatalf("full compiler packages not detected: %v", err)
	}
}

func TestQueryFailuresAreNotMissingPackages(t *testing.T) {
	for _, test := range []struct {
		name   string
		output string
		err    error
	}{
		{"command failed", "curl\tinstall ok installed\n", errors.New("database locked")},
		{"malformed record", "curl\tinstall ok installed\nbad record\n", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			restoreDependencies(t)
			commandExists = func(name string) bool { return name == "apt-get" }
			commandOutput = func(string, ...string) (string, error) { return test.output, test.err }
			commandRun = func(string, ...string) error {
				t.Fatal("failed query must not trigger batch installation")
				return nil
			}
			inventory := NewInventory()
			if _, _, err := inventory.InstalledCount(); err == nil {
				t.Fatal("query failure was reported as a successful count")
			}
			if badge := inventory.badge(toolByName(t, "curl")); !strings.Contains(badge, "检测失败") || strings.Contains(badge, "未安装") {
				t.Fatalf("incorrect failure badge: %q", badge)
			}
			if err := inventory.installMissing(newUIWithInput(t, "y\n")); err == nil {
				t.Fatal("batch installation ignored query failure")
			}
			commandOutput = func(string, ...string) (string, error) { return "curl\tinstall ok installed\n", nil }
			if err := inventory.Refresh(); err != nil || !inventory.toolInstalled(toolByName(t, "curl")) {
				t.Fatalf("refresh did not recover query failure: %v", err)
			}
		})
	}
}
