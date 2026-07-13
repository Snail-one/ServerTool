package status

import (
	"os"
	"path/filepath"
	"testing"

	commonbash "snail_tool/internal/common/bash"
	commonproxy "snail_tool/internal/common/proxy"
	commonvim "snail_tool/internal/common/vim"
	containerruntime "snail_tool/internal/container/runtime"
	"snail_tool/internal/system"
)

func TestDetectStatusForUserFiles(t *testing.T) {
	clearProxyEnv(t)

	home := t.TempDir()
	account := &system.Account{Name: "test", Home: home}

	if err := os.WriteFile(filepath.Join(home, ".vimrc"), []byte(commonvim.ManagedVimConfigContent()), 0644); err != nil {
		t.Fatal(err)
	}
	bashAliasBegin, bashAliasEnd := commonbash.BashAliasMarkers()
	proxyBegin, proxyEnd := commonproxy.ProxyMarkers()
	if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte(
		bashAliasBegin+"\n"+commonbash.BashAliasBlock()+"\n"+bashAliasEnd+"\n\n"+
			proxyBegin+"\nexport http_proxy=\"http://127.0.0.1:8888\"\n"+proxyEnd+"\n",
	), 0644); err != nil {
		t.Fatal(err)
	}

	status := DetectStatus(account)
	if !status.Vim {
		t.Fatal("expected Vim status to be configured")
	}
	if !status.Bash {
		t.Fatal("expected Bash status to be configured")
	}
	if !status.Proxy {
		t.Fatal("expected Proxy status to be configured")
	}
}

func TestRuntimeSummary(t *testing.T) {
	tests := []struct {
		name string
		in   []containerruntime.Runtime
		want string
	}{
		{name: "none", want: "未安装"},
		{name: "docker", in: []containerruntime.Runtime{{Name: "docker", Display: "Docker"}}, want: "Docker"},
		{name: "docker abnormal", in: []containerruntime.Runtime{{Name: "docker", Display: "Docker（服务异常）"}}, want: "Docker（服务异常）"},
		{name: "podman", in: []containerruntime.Runtime{{Name: "podman", Display: "Podman"}}, want: "Podman"},
		{name: "both", in: []containerruntime.Runtime{{Name: "docker", Display: "Docker"}, {Name: "podman", Display: "Podman"}}, want: "Docker、Podman；容器操作优先 Docker"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RuntimeSummary(tt.in); got != tt.want {
				t.Fatalf("summary = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetectStatusForMissingUserFiles(t *testing.T) {
	clearProxyEnv(t)

	account := &system.Account{Name: "test", Home: t.TempDir()}
	status := DetectStatus(account)

	if status.Vim || status.Bash || status.Proxy {
		t.Fatalf("expected missing files to be unconfigured, got %+v", status)
	}
}

func TestDetectStatusRequiresProxyURL(t *testing.T) {
	clearProxyEnv(t)

	home := t.TempDir()
	account := &system.Account{Name: "test", Home: home}

	proxyBegin, proxyEnd := commonproxy.ProxyMarkers()
	if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte(proxyBegin+"\n"+proxyEnd+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	status := DetectStatus(account)
	if status.Proxy {
		t.Fatal("expected empty proxy block to be unconfigured")
	}
}

func TestDetectStatusReadsProxyFileEnv(t *testing.T) {
	clearProxyEnv(t)

	home := t.TempDir()
	account := &system.Account{Name: "test", Home: home}
	if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte("https_proxy='http://192.168.31.108:52013'\n"), 0644); err != nil {
		t.Fatal(err)
	}

	status := DetectStatus(account)
	if !status.Proxy {
		t.Fatal("expected proxy file env to be configured")
	}
}

func TestDetectStatusIgnoresTemporaryProxyEnv(t *testing.T) {
	clearProxyEnv(t)
	t.Setenv("HTTP_PROXY", "http://192.168.31.108:52013")

	account := &system.Account{Name: "test", Home: t.TempDir()}
	status := DetectStatus(account)
	if status.Proxy {
		t.Fatal("expected temporary proxy env to be unconfigured")
	}
}

func clearProxyEnv(t *testing.T) {
	t.Helper()
	for _, name := range commonproxy.ProxyEnvNames() {
		t.Setenv(name, "")
	}
}
