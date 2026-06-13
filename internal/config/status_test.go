package config

import (
	"os"
	"path/filepath"
	"testing"

	"snail_tool/internal/system"
)

func TestDetectStatusForUserFiles(t *testing.T) {
	clearProxyEnv(t)

	home := t.TempDir()
	account := &system.Account{Name: "test", Home: home}

	if err := os.WriteFile(filepath.Join(home, ".vimrc"), []byte(vimrcContent), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte(
		bashAliasBegin+"\n"+bashAliasBlock+"\n"+bashAliasEnd+"\n\n"+
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

	if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte(proxyBegin+"\n"+proxyEnd+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	status := DetectStatus(account)
	if status.Proxy {
		t.Fatal("expected empty proxy block to be unconfigured")
	}
}

func TestDetectStatusReadsProxyEnv(t *testing.T) {
	clearProxyEnv(t)
	t.Setenv("HTTP_PROXY", "http://192.168.31.108:52013")

	account := &system.Account{Name: "test", Home: t.TempDir()}
	status := DetectStatus(account)
	if !status.Proxy {
		t.Fatal("expected proxy env to be configured")
	}
}

func clearProxyEnv(t *testing.T) {
	t.Helper()
	for _, name := range proxyEnvNames {
		t.Setenv(name, "")
	}
}
