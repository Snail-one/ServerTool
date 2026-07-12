package runtime

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/shared"
)

func TestRuntimeForCommands(t *testing.T) {
	tests := []struct {
		name      string
		docker    bool
		podman    bool
		wantName  string
		wantFound bool
	}{
		{name: "docker first", docker: true, podman: true, wantName: "docker", wantFound: true},
		{name: "podman only", podman: true, wantName: "podman", wantFound: true},
		{name: "missing", wantFound: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := runtimeForCommands(tt.docker, tt.podman)
			if ok != tt.wantFound {
				t.Fatalf("found = %v, want %v", ok, tt.wantFound)
			}
			if got.Name != tt.wantName {
				t.Fatalf("runtime name = %q, want %q", got.Name, tt.wantName)
			}
		})
	}
}

func TestDetectDockerDistributionMatrix(t *testing.T) {
	tests := []struct {
		id       string
		extra    string
		family   string
		wantFail bool
	}{
		{id: "debian", extra: "VERSION_CODENAME=bookworm\n", family: "apt"},
		{id: "ubuntu", extra: "VERSION_CODENAME=noble\n", family: "apt"},
		{id: "fedora", family: "rpm"},
		{id: "centos", family: "rpm"},
		{id: "rhel", family: "rpm"},
		{id: "arch", wantFail: true},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			installer := &dockerInstaller{readFile: func(string) ([]byte, error) {
				return []byte("ID=" + tt.id + "\nPRETTY_NAME=Test Linux\nVERSION_ID=1\n" + tt.extra), nil
			}, osReleasePath: "os-release"}
			got, err := installer.detectDistribution()
			if (err != nil) != tt.wantFail {
				t.Fatalf("error = %v, wantFail %v", err, tt.wantFail)
			}
			if err == nil && got.Family != tt.family {
				t.Fatalf("family = %q, want %q", got.Family, tt.family)
			}
		})
	}
}

func TestDockerKeyFingerprintSuccessAndFailure(t *testing.T) {
	installer := &dockerInstaller{output: func(string, ...string) (string, error) {
		return "pub:-:4096:1:KEY::::::\nfpr:::::::::" + dockerDebFingerprint + ":\n", nil
	}}
	fingerprint, err := installer.keyFingerprint("key")
	if err != nil || fingerprint != dockerDebFingerprint {
		t.Fatalf("fingerprint = %q, err = %v", fingerprint, err)
	}
	installer.output = func(string, ...string) (string, error) { return "not a key", nil }
	if _, err := installer.keyFingerprint("key"); err == nil {
		t.Fatal("expected missing fingerprint failure")
	}
}

func TestConfigureDockerRepositories(t *testing.T) {
	tests := []struct {
		name string
		dist dockerDistribution
		path string
		want string
	}{
		{name: "apt", dist: dockerDistribution{ID: "ubuntu", Family: "apt", Codename: "noble"}, path: "/etc/apt/sources.list.d/docker.sources", want: "Suites: noble"},
		{name: "rpm", dist: dockerDistribution{ID: "rhel", Family: "rpm"}, path: "/etc/yum.repos.d/docker-ce.repo", want: "linux/rhel/$releasever/$basearch/stable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writes := make(map[string]string)
			installer := &dockerInstaller{
				output:   func(string, ...string) (string, error) { return "amd64\n", nil },
				mkdirAll: func(string, os.FileMode) error { return nil },
				writeFile: func(path string, data []byte, _ shared.AtomicWriteOptions) error {
					writes[path] = string(data)
					return nil
				},
			}
			if err := installer.configureRepository(tt.dist, []byte("key")); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(writes[tt.path], tt.want) {
				t.Fatalf("repository = %q, want substring %q", writes[tt.path], tt.want)
			}
		})
	}
}

func TestDockerInstallConflictCancelDoesNotChangeSystem(t *testing.T) {
	installer, calls := fakeDockerInstaller(t)
	installer.output = func(name string, args ...string) (string, error) {
		if name == "dpkg-query" && args[len(args)-1] == "docker.io" {
			return "docker.io", nil
		}
		return "", errors.New("not installed")
	}
	installer.confirm = func(packages []string) (bool, error) {
		if !reflect.DeepEqual(packages, []string{"docker.io"}) {
			t.Fatalf("conflicts = %v", packages)
		}
		return false, nil
	}
	if err := installer.install(); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 0 {
		t.Fatalf("system-changing calls = %v", *calls)
	}
}

func TestDockerInstallFingerprintMismatchStopsBeforeRepository(t *testing.T) {
	installer, calls := fakeDockerInstaller(t)
	installer.output = dockerFakeOutput("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	if err := installer.install(); err == nil || !strings.Contains(err.Error(), "指纹不匹配") {
		t.Fatalf("error = %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("calls after mismatch = %v", *calls)
	}
}

func TestDockerInstallRunsAllStagesAndShortCircuits(t *testing.T) {
	installer, calls := fakeDockerInstaller(t)
	installer.output = dockerFakeOutput(dockerDebFingerprint)
	installer.run = func(name string, args ...string) error {
		*calls = append(*calls, strings.Join(append([]string{name}, args...), " "))
		if name == "apt-get" && reflect.DeepEqual(args, []string{"update"}) {
			return errors.New("metadata unavailable")
		}
		return nil
	}
	err := installer.install()
	if err == nil || !strings.Contains(err.Error(), "元数据刷新阶段") {
		t.Fatalf("error = %v", err)
	}
	for _, call := range *calls {
		if strings.Contains(call, " install ") || strings.HasPrefix(call, "systemctl") {
			t.Fatalf("did not short-circuit: %v", *calls)
		}
	}
}

func TestDockerInstallCompletesServiceStartAndVersionVerification(t *testing.T) {
	installer, calls := fakeDockerInstaller(t)
	installer.output = dockerFakeOutput(dockerDebFingerprint)
	if err := installer.install(); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(*calls, "\n")
	for _, wanted := range []string{
		"apt-get update",
		"apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin",
		"systemctl enable --now docker",
	} {
		if !strings.Contains(joined, wanted) {
			t.Fatalf("calls do not contain %q:\n%s", wanted, joined)
		}
	}
}

func fakeDockerInstaller(t *testing.T) (*dockerInstaller, *[]string) {
	t.Helper()
	var calls []string
	installer := &dockerInstaller{
		osReleasePath: "os-release",
		readFile: func(path string) ([]byte, error) {
			if path == "os-release" {
				return []byte("ID=debian\nPRETTY_NAME=Debian Test\nVERSION_ID=12\nVERSION_CODENAME=bookworm\n"), nil
			}
			return os.ReadFile(path)
		},
		download:      func(_ string, path string) error { return os.WriteFile(path, []byte("key"), 0600) },
		commandExists: func(name string) bool { return name == "gpg" || name == "apt-get" || name == "dpkg-query" },
		confirm:       func([]string) (bool, error) { return true, nil },
		mkdirAll:      func(string, os.FileMode) error { return nil },
		writeFile: func(path string, _ []byte, _ shared.AtomicWriteOptions) error {
			calls = append(calls, "write "+path)
			return nil
		},
		run: func(name string, args ...string) error {
			calls = append(calls, strings.Join(append([]string{name}, args...), " "))
			return nil
		},
	}
	return installer, &calls
}

func dockerFakeOutput(fingerprint string) func(string, ...string) (string, error) {
	return func(name string, args ...string) (string, error) {
		switch name {
		case "gpg":
			return "fpr:::::::::" + fingerprint + ":\n", nil
		case "dpkg":
			return "amd64\n", nil
		case "docker":
			return "Docker test version", nil
		default:
			return "", errors.New("not installed")
		}
	}
}
