package status

import (
	"os"
	"path/filepath"
	"testing"

	commonbash "snail_tool/internal/common/bash"
	commonproxy "snail_tool/internal/common/proxy"
	commonvim "snail_tool/internal/common/vim"
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
