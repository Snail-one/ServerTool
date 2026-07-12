package runtime

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestDockerUninstallPlanDetectsPackagesAndRepositoryFiles(t *testing.T) {
	uninstaller, _, _ := fakeDockerUninstaller()
	uninstaller.output = func(name string, args ...string) (string, error) {
		packageName := args[len(args)-1]
		if name == "dpkg-query" && (packageName == "docker-ce" || packageName == "containerd.io") {
			return packageName, nil
		}
		return "", errors.New("not installed")
	}
	uninstaller.fileExists = func(path string) bool {
		return path == "/etc/apt/sources.list.d/docker.sources" || path == "/etc/apt/keyrings/docker.asc"
	}

	plan, err := uninstaller.plan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.manager != "apt-get" {
		t.Fatalf("manager = %q", plan.manager)
	}
	if !reflect.DeepEqual(plan.packages, []string{"containerd.io", "docker-ce"}) {
		t.Fatalf("packages = %v", plan.packages)
	}
	wantFiles := []string{"/etc/apt/sources.list.d/docker.sources", "/etc/apt/keyrings/docker.asc"}
	if !reflect.DeepEqual(plan.repoFiles, wantFiles) {
		t.Fatalf("repo files = %v, want %v", plan.repoFiles, wantFiles)
	}
}

func TestDockerUninstallCancelChangesNothing(t *testing.T) {
	uninstaller, calls, removed := fakeDockerUninstaller()
	uninstaller.confirm = func(dockerUninstallPlan) (bool, error) { return false, nil }

	done, err := uninstaller.uninstall()
	if err != nil {
		t.Fatal(err)
	}
	if done {
		t.Fatal("canceled uninstall reported completion")
	}
	if len(*calls) != 0 || len(*removed) != 0 {
		t.Fatalf("cancel changed system: calls=%v removed=%v", *calls, *removed)
	}
}

func TestDockerUninstallDebCompletesInSafeOrder(t *testing.T) {
	uninstaller, calls, removed := fakeDockerUninstaller()
	done, err := uninstaller.uninstall()
	if err != nil {
		t.Fatal(err)
	}
	if !done {
		t.Fatal("uninstall did not report completion")
	}
	wantCalls := []string{
		"systemctl disable --now docker.service docker.socket",
		"apt-get purge -y docker-ce docker-ce-cli",
	}
	if !reflect.DeepEqual(*calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", *calls, wantCalls)
	}
	wantRemoved := []string{"/etc/apt/sources.list.d/docker.sources", "/etc/apt/keyrings/docker.asc"}
	if !reflect.DeepEqual(*removed, wantRemoved) {
		t.Fatalf("removed = %v, want %v", *removed, wantRemoved)
	}
}

func TestDockerUninstallPackageFailureKeepsRepository(t *testing.T) {
	uninstaller, calls, removed := fakeDockerUninstaller()
	uninstaller.run = func(name string, args ...string) error {
		*calls = append(*calls, strings.Join(append([]string{name}, args...), " "))
		if name == "apt-get" {
			return errors.New("purge failed")
		}
		return nil
	}

	done, err := uninstaller.uninstall()
	if done || err == nil || !strings.Contains(err.Error(), "软件包删除阶段") {
		t.Fatalf("done=%v error=%v", done, err)
	}
	if len(*removed) != 0 {
		t.Fatalf("repository removed after package failure: %v", *removed)
	}
}

func TestDockerUninstallRPMUsesRemove(t *testing.T) {
	uninstaller, calls, _ := fakeDockerUninstaller()
	uninstaller.readFile = func(string) ([]byte, error) {
		return []byte("ID=fedora\nPRETTY_NAME=Fedora Test\nVERSION_ID=42\n"), nil
	}
	uninstaller.commandExists = func(name string) bool {
		return name == "dnf" || name == "rpm"
	}
	uninstaller.output = func(name string, args ...string) (string, error) {
		if name == "rpm" && args[len(args)-1] == "docker-ce" {
			return "docker-ce", nil
		}
		return "", errors.New("not installed")
	}
	uninstaller.fileExists = func(string) bool { return false }

	if done, err := uninstaller.uninstall(); err != nil || !done {
		t.Fatalf("done=%v error=%v", done, err)
	}
	if got := (*calls)[1]; got != "dnf remove -y docker-ce" {
		t.Fatalf("remove call = %q", got)
	}
}

func TestDockerUninstallNothingDetected(t *testing.T) {
	uninstaller, _, _ := fakeDockerUninstaller()
	uninstaller.output = func(string, ...string) (string, error) { return "", errors.New("not installed") }
	uninstaller.fileExists = func(string) bool { return false }
	if done, err := uninstaller.uninstall(); done || err == nil || !strings.Contains(err.Error(), "未检测到") {
		t.Fatalf("done=%v error=%v", done, err)
	}
}

func TestSelectDockerUninstallMode(t *testing.T) {
	tests := []struct {
		name       string
		answers    []string
		removeData bool
		proceed    bool
	}{
		{name: "safe", answers: []string{"1"}, proceed: true},
		{name: "complete after invalid", answers: []string{"bad", "2"}, removeData: true, proceed: true},
		{name: "cancel", answers: []string{"q"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := &fakeDockerUninstallPrompt{answers: tt.answers}
			removeData, proceed, err := selectDockerUninstallMode(prompt)
			if err != nil {
				t.Fatal(err)
			}
			if removeData != tt.removeData || proceed != tt.proceed {
				t.Fatalf("removeData=%v proceed=%v", removeData, proceed)
			}
		})
	}
}

func TestCompleteDockerUninstallRequiresExactConfirmation(t *testing.T) {
	plan := dockerUninstallPlan{removeData: true, dataPaths: []string{"/var/lib/docker"}}
	for _, tt := range []struct {
		answer string
		want   bool
	}{
		{answer: "DELETE", want: false},
		{answer: "delete docker data", want: false},
		{answer: "DELETE DOCKER DATA", want: true},
	} {
		prompt := &fakeDockerUninstallPrompt{answers: []string{tt.answer}}
		got, err := confirmDockerUninstall(prompt, plan)
		if err != nil {
			t.Fatal(err)
		}
		if got != tt.want {
			t.Fatalf("answer %q: confirmed=%v, want %v", tt.answer, got, tt.want)
		}
	}
}

func TestCompleteDockerUninstallDeletesDataAndRefreshesSystemd(t *testing.T) {
	uninstaller, calls, _ := fakeDockerUninstaller()
	uninstaller.removeData = true
	uninstaller.pathExists = func(path string) bool {
		return path == "/var/lib/docker" || path == "/etc/docker" || path == "/etc/systemd/system/docker.service.d"
	}
	var removedTrees []string
	uninstaller.removeTree = func(path string) error {
		removedTrees = append(removedTrees, path)
		return nil
	}

	done, err := uninstaller.uninstall()
	if err != nil || !done {
		t.Fatalf("done=%v error=%v", done, err)
	}
	wantTrees := []string{"/var/lib/docker", "/etc/docker", "/etc/systemd/system/docker.service.d"}
	if !reflect.DeepEqual(removedTrees, wantTrees) {
		t.Fatalf("removed trees = %v, want %v", removedTrees, wantTrees)
	}
	joined := strings.Join(*calls, "\n")
	if !strings.Contains(joined, "systemctl daemon-reload") || !strings.Contains(joined, "systemctl reset-failed") {
		t.Fatalf("systemd was not refreshed: %v", *calls)
	}
}

type fakeDockerUninstallPrompt struct {
	answers []string
	confirm bool
}

func (prompt *fakeDockerUninstallPrompt) Ask(string) (string, error) {
	if len(prompt.answers) == 0 {
		return "", errors.New("no answer")
	}
	answer := prompt.answers[0]
	prompt.answers = prompt.answers[1:]
	return answer, nil
}

func (prompt *fakeDockerUninstallPrompt) Confirm(string) (bool, error) {
	return prompt.confirm, nil
}

func fakeDockerUninstaller() (*dockerUninstaller, *[]string, *[]string) {
	var calls []string
	var removed []string
	installed := map[string]bool{"docker-ce": true, "docker-ce-cli": true}
	uninstaller := &dockerUninstaller{
		osReleasePath: "os-release",
		readFile: func(string) ([]byte, error) {
			return []byte("ID=debian\nPRETTY_NAME=Debian Test\nVERSION_ID=12\nVERSION_CODENAME=bookworm\n"), nil
		},
		commandExists: func(name string) bool {
			return name == "apt-get" || name == "dpkg-query"
		},
		output: func(name string, args ...string) (string, error) {
			packageName := args[len(args)-1]
			if name == "dpkg-query" && installed[packageName] {
				return packageName, nil
			}
			return "", errors.New("not installed")
		},
		run: func(name string, args ...string) error {
			calls = append(calls, strings.Join(append([]string{name}, args...), " "))
			return nil
		},
		fileExists: func(path string) bool {
			return path == "/etc/apt/sources.list.d/docker.sources" || path == "/etc/apt/keyrings/docker.asc"
		},
		pathExists: func(string) bool { return false },
		removeFile: func(path string) error {
			removed = append(removed, path)
			return nil
		},
		removeTree: func(string) error { return nil },
		confirm:    func(dockerUninstallPlan) (bool, error) { return true, nil },
	}
	return uninstaller, &calls, &removed
}
