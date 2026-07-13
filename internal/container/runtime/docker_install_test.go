package runtime

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/shared"
)

func TestDockerSupportedPlatformPolicy(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		version string
		pretty  string
		arch    string
		wantErr bool
	}{
		{name: "ubuntu 26.04", id: "ubuntu", version: "26.04", arch: "x86_64"},
		{name: "ubuntu 25.10", id: "ubuntu", version: "25.10", arch: "x86_64"},
		{name: "ubuntu 24.04", id: "ubuntu", version: "24.04", arch: "s390x"},
		{name: "ubuntu 22.04 arm", id: "ubuntu", version: "22.04", arch: "armv7l"},
		{name: "debian 13", id: "debian", version: "13", arch: "aarch64"},
		{name: "debian 12", id: "debian", version: "12", arch: "x86_64"},
		{name: "debian 11 ppc", id: "debian", version: "11", arch: "ppc64le"},
		{name: "fedora 44", id: "fedora", version: "44", arch: "x86_64"},
		{name: "fedora 43", id: "fedora", version: "43", arch: "ppc64le"},
		{name: "centos stream 10", id: "centos", version: "10", pretty: "CentOS Stream 10", arch: "aarch64"},
		{name: "centos stream 9", id: "centos", version: "9", pretty: "CentOS Stream 9", arch: "ppc64le"},
		{name: "rhel 10", id: "rhel", version: "10", arch: "aarch64"},
		{name: "rhel 9", id: "rhel", version: "9", arch: "x86_64"},
		{name: "rhel 8", id: "rhel", version: "8", arch: "s390x"},
		{name: "old ubuntu", id: "ubuntu", version: "20.04", arch: "x86_64", wantErr: true},
		{name: "unknown debian", id: "debian", version: "14", arch: "x86_64", wantErr: true},
		{name: "centos not stream", id: "centos", version: "9", pretty: "CentOS Linux 9", arch: "x86_64", wantErr: true},
		{name: "unsupported rhel ppc", id: "rhel", version: "9", arch: "ppc64le", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			installer := &dockerInstaller{
				commandExists: func(name string) bool { return name == "dnf" },
				output: func(name string, args ...string) (string, error) {
					if name == "uname" {
						return tt.arch, nil
					}
					if name == "dnf" {
						return "centos-stream-extras-common", nil
					}
					return "", errors.New("unexpected command")
				},
			}
			pretty := tt.pretty
			if pretty == "" {
				pretty = tt.id
			}
			dist := dockerDistribution{ID: tt.id, Version: tt.version, Name: pretty}
			err := installer.validatePlatform(&dist)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validatePlatform() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDuplicateRepositoryScannerAllowsManagedFileAndRejectsExtra(t *testing.T) {
	dir := t.TempDir()
	managed := dir + "/docker.sources"
	extra := dir + "/docker.list"
	if err := os.WriteFile(managed, []byte("URIs: https://download.docker.com/linux/ubuntu\n"), 0600); err != nil {
		t.Fatal(err)
	}
	installer := &dockerInstaller{readDir: os.ReadDir, readFile: os.ReadFile}
	dist := dockerDistribution{ID: "ubuntu", Family: "apt"}
	if err := installer.checkRepositoryDirectory(dir, "docker.sources", dist); err != nil {
		t.Fatalf("managed repository should be updatable: %v", err)
	}
	if err := os.WriteFile(extra, []byte("deb https://download.docker.com/linux/ubuntu noble stable\n"), 0600); err != nil {
		t.Fatal(err)
	}
	err := installer.checkRepositoryDirectory(dir, "docker.sources", dist)
	if err == nil || !strings.Contains(err.Error(), "docker.list") {
		t.Fatalf("extra repository error = %v", err)
	}
}

func TestDockerConflictPackagesByDistribution(t *testing.T) {
	tests := []struct {
		id       string
		contains []string
		excludes []string
	}{
		{id: "debian", contains: []string{"docker.io", "podman-docker", "runc"}, excludes: []string{"docker-compose-v2"}},
		{id: "ubuntu", contains: []string{"docker.io", "docker-compose-v2", "runc"}},
		{id: "fedora", contains: []string{"docker-selinux", "docker-engine-selinux"}, excludes: []string{"podman", "runc"}},
		{id: "centos", contains: []string{"docker-engine"}, excludes: []string{"podman", "runc", "docker-selinux"}},
		{id: "rhel", contains: []string{"docker-engine", "podman", "runc"}},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			got := dockerConflictPackages[tt.id]
			for _, name := range tt.contains {
				if !containsString(got, name) {
					t.Errorf("%s conflicts missing %s: %v", tt.id, name, got)
				}
			}
			for _, name := range tt.excludes {
				if containsString(got, name) {
					t.Errorf("%s conflicts unexpectedly contain %s: %v", tt.id, name, got)
				}
			}
		})
	}
}

func TestUbuntuRepositorySuitePrefersUbuntuCodename(t *testing.T) {
	tests := []struct {
		name           string
		ubuntuCodename string
		versionName    string
		want           string
	}{
		{name: "ubuntu codename", ubuntuCodename: "noble", versionName: "oracular", want: "noble"},
		{name: "fallback", versionName: "jammy", want: "jammy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := "ID=ubuntu\nVERSION_ID=24.04\nVERSION_CODENAME=" + tt.versionName + "\nUBUNTU_CODENAME=" + tt.ubuntuCodename + "\n"
			installer := &dockerInstaller{osReleasePath: "release", readFile: func(string) ([]byte, error) { return []byte(content), nil }}
			dist, err := installer.detectDistribution()
			if err != nil {
				t.Fatal(err)
			}
			if dist.RepositorySuite != tt.want {
				t.Fatalf("suite = %q, want %q", dist.RepositorySuite, tt.want)
			}
		})
	}
}

func TestAPTInstalledStateIgnoresResidualConfig(t *testing.T) {
	states := map[string]string{"installed": "ii  docker.io", "residual": "rc  docker-compose", "unpacked": "iU  containerd"}
	installer := &dockerInstaller{output: func(_ string, args ...string) (string, error) {
		return states[args[len(args)-1]], nil
	}}
	dist := dockerDistribution{ID: "debian", Family: "apt"}
	if ok, _ := installer.packageInstalled(dist, "installed"); !ok {
		t.Fatal("installed package was not detected")
	}
	for _, name := range []string{"residual", "unpacked"} {
		if ok, _ := installer.packageInstalled(dist, name); ok {
			t.Fatalf("state %s was incorrectly treated as installed", name)
		}
	}
}

func TestDuplicateRepositoryStopsBeforeConfirmation(t *testing.T) {
	installer, calls := fakeDockerInstaller(t)
	confirmed := false
	installer.repositoryCheck = func(dockerDistribution) error { return errors.New("duplicate docker.list") }
	installer.confirm = func([]string) (bool, error) {
		confirmed = true
		return true, nil
	}
	err := installer.install()
	if err == nil || !strings.Contains(err.Error(), "重复仓库") {
		t.Fatalf("error = %v", err)
	}
	if confirmed || len(*calls) != 0 {
		t.Fatalf("preflight failure changed state: confirmed=%v calls=%v", confirmed, *calls)
	}
}

func TestUnsupportedPlatformStopsWithoutChanges(t *testing.T) {
	installer, calls := fakeDockerInstaller(t)
	confirmed := false
	installer.platformCheck = func(*dockerDistribution) error { return errors.New("unsupported architecture") }
	installer.confirm = func([]string) (bool, error) {
		confirmed = true
		return true, nil
	}
	err := installer.install()
	if err == nil || !strings.Contains(err.Error(), "平台预检") {
		t.Fatalf("error = %v", err)
	}
	if confirmed || len(*calls) != 0 {
		t.Fatalf("unsupported platform changed state: confirmed=%v calls=%v", confirmed, *calls)
	}
}

func TestDependencyFailureStopsBeforeKeyAndRepository(t *testing.T) {
	installer, calls := fakeDockerInstaller(t)
	installer.output = dockerFakeOutput(dockerDebFingerprint)
	downloaded := false
	installer.download = func(string, string) error { downloaded = true; return nil }
	installer.dependencyCheck = func(dockerDistribution) ([]string, error) { return []string{"gnupg"}, nil }
	installer.run = func(name string, args ...string) error {
		*calls = append(*calls, strings.Join(append([]string{name}, args...), " "))
		if reflect.DeepEqual(args, []string{"install", "-y", "gnupg"}) {
			return errors.New("package unavailable")
		}
		return nil
	}
	err := installer.install()
	if err == nil || !strings.Contains(err.Error(), "前置依赖") {
		t.Fatalf("error = %v", err)
	}
	if downloaded {
		t.Fatal("key downloaded after dependency failure")
	}
	for _, call := range *calls {
		if strings.HasPrefix(call, "write ") {
			t.Fatalf("repository written after dependency failure: %v", *calls)
		}
	}
}

func TestDockerInstallStageOrder(t *testing.T) {
	installer, calls := fakeDockerInstaller(t)
	baseOutput := dockerFakeOutput(dockerDebFingerprint)
	installer.output = func(name string, args ...string) (string, error) {
		if name == "dpkg-query" && args[len(args)-1] == "docker.io" {
			return "ii docker.io", nil
		}
		return baseOutput(name, args...)
	}
	if err := installer.install(); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(*calls, "\n")
	ordered := []string{
		"apt-get update",
		"write /etc/apt/keyrings/docker.asc",
		"write /etc/apt/sources.list.d/docker.sources",
		"apt-get update",
		"apt-get remove -y docker.io",
		"apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin",
		"systemctl enable --now docker",
	}
	position := -1
	for _, wanted := range ordered {
		next := strings.Index(joined[position+1:], wanted)
		if next < 0 {
			t.Fatalf("missing ordered stage %q:\n%s", wanted, joined)
		}
		position += next + 1
	}
}

func TestHelloWorldOutcomes(t *testing.T) {
	tests := []struct {
		name      string
		runOnline bool
		onlineErr error
		wantErr   bool
	}{
		{name: "skipped"},
		{name: "success", runOnline: true},
		{name: "pull failure", runOnline: true, onlineErr: errors.New("hub unavailable"), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			installer, _ := fakeDockerInstaller(t)
			installer.output = dockerFakeOutput(dockerDebFingerprint)
			installer.confirmOnline = func() (bool, error) { return tt.runOnline, nil }
			installer.onlineVerify = func() error { return tt.onlineErr }
			err := installer.install()
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "联网/容器运行验证失败") {
				t.Fatalf("wrong online failure: %v", err)
			}
		})
	}
}

func TestConfigureRepositoryWritesOnlyStableRPMSection(t *testing.T) {
	writes := map[string]string{}
	installer := &dockerInstaller{
		mkdirAll: func(string, os.FileMode) error { return nil },
		writeFile: func(path string, data []byte, _ shared.AtomicWriteOptions) error {
			writes[path] = string(data)
			return nil
		},
	}
	err := installer.configureRepository(dockerDistribution{ID: "fedora", Family: "rpm"}, []byte("key"))
	if err != nil {
		t.Fatal(err)
	}
	repo := writes["/etc/yum.repos.d/docker-ce.repo"]
	if strings.Count(repo, "[") != 1 || !strings.Contains(repo, "[docker-ce-stable]") || strings.Contains(repo, "test") || strings.Contains(repo, "source") {
		t.Fatalf("unexpected RPM repository sections: %q", repo)
	}
}
