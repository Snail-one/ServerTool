package proxy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	commonproxy "snail_tool/internal/common/proxy"
	"snail_tool/internal/system"
)

func TestRunRemovesManagedBlockAndCurrentEnv(t *testing.T) {
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

	if err := Run(account); err != nil {
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

func TestRunKeepsEmptyManagedFile(t *testing.T) {
	clearProxyEnv(t)

	home := t.TempDir()
	account := &system.Account{Name: "test", Home: home}
	bashrc := filepath.Join(home, ".bashrc")
	content := proxyBegin + "\nexport http_proxy=\"http://127.0.0.1:8888\"\n" + proxyEnd + "\n"
	if err := os.WriteFile(bashrc, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Run(account); err != nil {
		t.Fatal(err)
	}

	if got := readTestFile(t, bashrc); got != "" {
		t.Fatalf("expected managed .bashrc to be kept empty, got:\n%s", got)
	}
}

func clearProxyEnv(t *testing.T) {
	t.Helper()
	for _, name := range commonproxy.ProxyCleanupEnvNames() {
		t.Setenv(name, "")
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
