package runtime

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/system"
)

func TestPodmanUninstallSafeModePreservesData(t *testing.T) {
	uninstaller, calls, removed := fakePodmanUninstaller(false)
	done, err := uninstaller.uninstall()
	if err != nil || !done {
		t.Fatalf("done=%v error=%v", done, err)
	}
	wantCalls := []string{"apt-get purge -y podman podman-compose"}
	if !reflect.DeepEqual(*calls, wantCalls) {
		t.Fatalf("calls=%v want=%v", *calls, wantCalls)
	}
	if len(*removed) != 0 {
		t.Fatalf("safe uninstall removed configuration: %v", *removed)
	}
}

func TestPodmanUninstallCompleteResetsRootAndUserBeforePackageRemoval(t *testing.T) {
	uninstaller, calls, removed := fakePodmanUninstaller(true)
	done, err := uninstaller.uninstall()
	if err != nil || !done {
		t.Fatalf("done=%v error=%v", done, err)
	}
	wantCalls := []string{
		"podman system reset --force",
		"as alice podman system reset --force",
		"apt-get purge -y podman podman-compose",
	}
	if !reflect.DeepEqual(*calls, wantCalls) {
		t.Fatalf("calls=%v want=%v", *calls, wantCalls)
	}
	wantRemoved := []string{"/root/.config/containers", "/home/alice/.config/containers"}
	if !reflect.DeepEqual(*removed, wantRemoved) {
		t.Fatalf("removed=%v want=%v", *removed, wantRemoved)
	}
}

func TestPodmanUninstallCancelChangesNothing(t *testing.T) {
	uninstaller, calls, removed := fakePodmanUninstaller(true)
	uninstaller.confirm = func(podmanUninstallPlan) (bool, error) { return false, nil }
	done, err := uninstaller.uninstall()
	if err != nil {
		t.Fatal(err)
	}
	if done || len(*calls) != 0 || len(*removed) != 0 {
		t.Fatalf("cancel changed system: done=%v calls=%v removed=%v", done, *calls, *removed)
	}
}

func TestPodmanUninstallResetFailureKeepsPackages(t *testing.T) {
	uninstaller, calls, removed := fakePodmanUninstaller(true)
	uninstaller.run = func(name string, args ...string) error {
		*calls = append(*calls, strings.Join(append([]string{name}, args...), " "))
		if name == "podman" {
			return errors.New("reset failed")
		}
		return nil
	}
	done, err := uninstaller.uninstall()
	if done || err == nil || !strings.Contains(err.Error(), "rootful 数据清理阶段") {
		t.Fatalf("done=%v error=%v", done, err)
	}
	if len(*calls) != 1 || len(*removed) != 0 {
		t.Fatalf("continued after reset failure: calls=%v removed=%v", *calls, *removed)
	}
}

func TestSelectPodmanUninstallMode(t *testing.T) {
	for _, tt := range []struct {
		name       string
		answers    []string
		removeData bool
		proceed    bool
	}{
		{name: "safe", answers: []string{"1"}, proceed: true},
		{name: "complete", answers: []string{"bad", "2"}, removeData: true, proceed: true},
		{name: "cancel", answers: []string{"q"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			prompt := &fakeDockerUninstallPrompt{answers: tt.answers}
			removeData, proceed, err := selectPodmanUninstallMode(prompt)
			if err != nil {
				t.Fatal(err)
			}
			if removeData != tt.removeData || proceed != tt.proceed {
				t.Fatalf("removeData=%v proceed=%v", removeData, proceed)
			}
		})
	}
}

func TestCompletePodmanUninstallRequiresExactConfirmation(t *testing.T) {
	plan := podmanUninstallPlan{removeData: true}
	for _, tt := range []struct {
		answer string
		want   bool
	}{
		{answer: "DELETE", want: false},
		{answer: "delete podman data", want: false},
		{answer: "DELETE PODMAN DATA", want: true},
	} {
		prompt := &fakeDockerUninstallPrompt{answers: []string{tt.answer}}
		got, err := confirmPodmanUninstall(prompt, plan)
		if err != nil {
			t.Fatal(err)
		}
		if got != tt.want {
			t.Fatalf("answer %q: confirmed=%v want=%v", tt.answer, got, tt.want)
		}
	}
}

func TestPodmanPackageToolsSupportsAPTAndRPM(t *testing.T) {
	for _, tt := range []struct {
		name     string
		commands map[string]bool
		manager  string
		family   string
		query    string
	}{
		{name: "apt", commands: map[string]bool{"apt-get": true, "dpkg-query": true}, manager: "apt-get", family: "apt", query: "dpkg-query"},
		{name: "dnf", commands: map[string]bool{"dnf": true, "rpm": true}, manager: "dnf", family: "rpm", query: "rpm"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			manager, family, query, err := podmanPackageTools(func(name string) bool { return tt.commands[name] })
			if err != nil || manager != tt.manager || family != tt.family || query != tt.query {
				t.Fatalf("manager=%q family=%q query=%q error=%v", manager, family, query, err)
			}
		})
	}
}

func fakePodmanUninstaller(removeData bool) (*podmanUninstaller, *[]string, *[]string) {
	var calls []string
	var removed []string
	installed := map[string]bool{"podman": true, "podman-compose": true}
	uninstaller := &podmanUninstaller{
		removeData: removeData,
		commandExists: func(name string) bool {
			return name == "apt-get" || name == "dpkg-query" || name == "podman" || name == "runuser"
		},
		output: func(name string, args ...string) (string, error) {
			if name == "dpkg-query" {
				packageName := args[len(args)-1]
				if installed[packageName] {
					return packageName, nil
				}
			}
			return "", errors.New("not installed")
		},
		run: func(name string, args ...string) error {
			calls = append(calls, strings.Join(append([]string{name}, args...), " "))
			return nil
		},
		runAs: func(account *system.Account, name string, args ...string) error {
			calls = append(calls, "as "+account.Name+" "+strings.Join(append([]string{name}, args...), " "))
			return nil
		},
		targetUser: func() (*system.Account, error) {
			return &system.Account{Name: "alice", Home: "/home/alice", UID: 1000, GID: 1000}, nil
		},
		pathExists: func(path string) bool {
			return path == "/root/.config/containers" || path == "/home/alice/.config/containers"
		},
		removeTree: func(path string) error {
			removed = append(removed, path)
			return nil
		},
		confirm: func(podmanUninstallPlan) (bool, error) { return true, nil },
	}
	return uninstaller, &calls, &removed
}
