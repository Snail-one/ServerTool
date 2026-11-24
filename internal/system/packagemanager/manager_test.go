package packagemanager

import (
	"reflect"
	"testing"
)

func TestDetectPackageManager(t *testing.T) {
	for _, test := range []Manager{
		{Name: "apt-get", RefreshArgs: []string{"update"}, InstallArgs: []string{"install", "-y"}},
		{Name: "dnf", InstallArgs: []string{"install", "-y"}},
		{Name: "pacman", InstallArgs: []string{"-Sy", "--noconfirm", "--needed"}},
		{Name: "zypper", InstallArgs: []string{"--non-interactive", "install"}},
		{Name: "apk", InstallArgs: []string{"add"}},
	} {
		t.Run(test.Name, func(t *testing.T) {
			commandExists := func(name string) bool { return name == test.Name }
			manager, err := Detect(commandExists)
			if err != nil || !reflect.DeepEqual(manager, test) {
				t.Fatalf("manager=%#v err=%v, want %#v", manager, err, test)
			}
		})
	}
}

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
			queries := 0
			commandOutput := func(name string, args ...string) (string, error) {
				queries++
				if name != test.command || !reflect.DeepEqual(args, test.args) {
					t.Fatalf("unexpected query: %s %v", name, args)
				}
				return test.output, nil
			}
			got, err := Installed(Manager{Name: test.manager}, commandOutput)
			want := map[string]bool{"curl": true, "bash-completion": true}
			if err != nil || queries != 1 || !reflect.DeepEqual(got, want) {
				t.Fatalf("installed=%v queries=%d err=%v, want %v", got, queries, err, want)
			}
		})
	}
}
