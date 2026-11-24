package proxy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"snail_tool/internal/system"
)

func TestCleanupRemovesManagedBlockAndCurrentEnv(t *testing.T) {
	clearProxyEnv(t)
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:8888")
	t.Setenv("NO_PROXY", "localhost,127.0.0.1")
	t.Setenv("no_proxy", "localhost,127.0.0.1")

	home := t.TempDir()
	account := &system.Account{Name: "test", Home: home}
	bashrc := filepath.Join(home, ".bashrc")
	content := "export EDITOR=vim\n\n" +
		proxyBegin + "\nexport http_proxy=\"http://127.0.0.1:8888\"\n" + proxyEnd + "\n"
	if err := os.WriteFile(bashrc, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Cleanup(account); err != nil {
		t.Fatal(err)
	}

	got := readTestFile(t, bashrc)
	if strings.Contains(got, proxyBegin) || strings.Contains(got, "http_proxy") {
		t.Fatalf("managed proxy content remained:\n%s", got)
	}
	if !strings.Contains(got, "export EDITOR=vim") {
		t.Fatalf("unrelated bash content was removed:\n%s", got)
	}
	if value := os.Getenv("HTTP_PROXY"); value != "" {
		t.Fatalf("HTTP_PROXY was not unset, got %q", value)
	}
	if value := os.Getenv("NO_PROXY"); value != "" {
		t.Fatalf("NO_PROXY was not unset, got %q", value)
	}
	if value := os.Getenv("no_proxy"); value != "" {
		t.Fatalf("no_proxy was not unset, got %q", value)
	}
}

func TestCleanupKeepsEmptyManagedFile(t *testing.T) {
	clearProxyEnv(t)

	home := t.TempDir()
	account := &system.Account{Name: "test", Home: home}
	bashrc := filepath.Join(home, ".bashrc")
	content := proxyBegin + "\nexport http_proxy=\"http://127.0.0.1:8888\"\n" + proxyEnd + "\n"
	if err := os.WriteFile(bashrc, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Cleanup(account); err != nil {
		t.Fatal(err)
	}

	if got := readTestFile(t, bashrc); got != "" {
		t.Fatalf("expected managed .bashrc to be kept empty, got:\n%s", got)
	}
}
