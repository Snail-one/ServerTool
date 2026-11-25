package status

import (
	"os"
	"path/filepath"
	"testing"

	commonbash "snail_tool/internal/configuration/bash"
	commonproxy "snail_tool/internal/configuration/proxy"
	commonvim "snail_tool/internal/configuration/vim"
	"snail_tool/internal/storage"
	"snail_tool/internal/system"
)

func TestDetectStatusForUserFiles(t *testing.T) {
	clearProxyEnv(t)

	home := t.TempDir()
	account := &system.Account{Name: "test", Home: home}

	vimBegin, vimEnd := commonvim.VimMarkers()
	if err := os.WriteFile(filepath.Join(home, ".vimrc"), []byte(
		"set number\n\n"+storage.FormatManagedBlock(vimBegin, commonvim.ManagedVimConfigContent(), vimEnd),
	), 0644); err != nil {
		t.Fatal(err)
	}
	bashAliasBegin, bashAliasEnd := commonbash.BashAliasMarkers()
	proxyBegin, proxyEnd := commonproxy.ProxyMarkers()
	if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte(
		bashAliasBegin+"\n"+commonbash.BashManagedBlock()+"\n"+bashAliasEnd+"\n\n"+
			proxyBegin+"\nexport http_proxy=\"http://127.0.0.1:8888\"\n"+proxyEnd+"\n",
	), 0644); err != nil {
		t.Fatal(err)
	}

	status := DetectStatusWithProgress(account, nil)
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

func TestUserConfigurationCountExcludesUPS(t *testing.T) {
	clearProxyEnv(t)

	home := t.TempDir()
	account := &system.Account{Name: "test", Home: home}
	status := DetectStatusWithProgress(account, nil)

	if status.ConfigTotal != 3 {
		t.Fatalf("system/user configuration total = %d, want 3", status.ConfigTotal)
	}
	if status.UPS && status.Configured != 0 {
		t.Fatalf("UPS was counted as a system/user configuration: %+v", status)
	}
}

func TestDetectStatusForMissingUserFiles(t *testing.T) {
	clearProxyEnv(t)

	account := &system.Account{Name: "test", Home: t.TempDir()}
	status := DetectStatusWithProgress(account, nil)

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

	status := DetectStatusWithProgress(account, nil)
	if status.Proxy {
		t.Fatal("expected empty proxy block to be unconfigured")
	}
}

func TestDetectStatusReadsProxyFileEnv(t *testing.T) {
	clearProxyEnv(t)

	home := t.TempDir()
	account := &system.Account{Name: "test", Home: home}
	if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte("https_proxy='http://203.0.113.10:8080'\n"), 0644); err != nil {
		t.Fatal(err)
	}

	status := DetectStatusWithProgress(account, nil)
	if !status.Proxy {
		t.Fatal("expected proxy file env to be configured")
	}
}

func TestDetectStatusIgnoresTemporaryProxyEnv(t *testing.T) {
	clearProxyEnv(t)
	t.Setenv("HTTP_PROXY", "http://203.0.113.10:8080")

	account := &system.Account{Name: "test", Home: t.TempDir()}
	status := DetectStatusWithProgress(account, nil)
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
