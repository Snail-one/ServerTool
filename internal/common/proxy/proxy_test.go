package proxy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"snail_tool/internal/shared"
	"snail_tool/internal/system"
)

func TestMaskProxyURL(t *testing.T) {
	got := maskProxyURL("http://admin:secret@192.168.1.1:8888")
	want := "http://admin:******@192.168.1.1:8888"
	if got != want {
		t.Fatalf("maskProxyURL() = %q, want %q", got, want)
	}

	plain := "http://127.0.0.1:8888"
	if got := maskProxyURL(plain); got != plain {
		t.Fatalf("maskProxyURL() = %q, want %q", got, plain)
	}

	withSlash := maskProxyURL("http://admin:secret@192.168.1.100:7890/")
	if withSlash != "http://admin:******@192.168.1.100:7890/" {
		t.Fatalf("maskProxyURL() = %q", withSlash)
	}
}

func TestWriteProxyBlockIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bashrc")
	if err := os.WriteFile(path, []byte("export EDITOR=vim\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := writeProxyBlockReplacingUnmanaged(path, "http://127.0.0.1:8888"); err != nil {
		t.Fatal(err)
	}
	if err := writeProxyBlockReplacingUnmanaged(path, "http://admin:secret@192.168.1.1:8888"); err != nil {
		t.Fatal(err)
	}

	content := readTestFile(t, path)
	if strings.Count(content, proxyBegin) != 1 || strings.Count(content, proxyEnd) != 1 {
		t.Fatalf("proxy block is not idempotent:\n%s", content)
	}
	if strings.Contains(content, "127.0.0.1:8888") {
		t.Fatalf("old proxy URL remained:\n%s", content)
	}
	if !strings.Contains(content, "http://admin:secret@192.168.1.1:8888") {
		t.Fatalf("new proxy URL missing:\n%s", content)
	}
	wantBody := `export http_proxy="http://admin:secret@192.168.1.1:8888"
export https_proxy="http://admin:secret@192.168.1.1:8888"

export HTTP_PROXY="http://admin:secret@192.168.1.1:8888"
export HTTPS_PROXY="http://admin:secret@192.168.1.1:8888"

export no_proxy="` + noProxy + `"
export NO_PROXY="` + noProxy + `"`
	if wantBlock := shared.FormatManagedBlock(proxyBegin, wantBody, proxyEnd); !strings.Contains(content, wantBlock) {
		t.Fatalf("managed proxy block spacing mismatch:\n%s", content)
	}
	if !strings.Contains(content, "export EDITOR=vim") {
		t.Fatalf("unrelated bashrc content was removed:\n%s", content)
	}
}

func TestWriteProxyBlockReplacingUnmanagedRemovesUserProxyLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bashrc")
	input := `export EDITOR=vim
export http_proxy="http://old.example:8080"
https_proxy='http://old.example:8080'
export no_proxy="localhost"
alias ll='ls -lah'
`
	if err := os.WriteFile(path, []byte(input), 0644); err != nil {
		t.Fatal(err)
	}

	if err := writeProxyBlockReplacingUnmanaged(path, "http://127.0.0.1:8888"); err != nil {
		t.Fatal(err)
	}

	content := readTestFile(t, path)
	for _, old := range []string{"old.example", "export no_proxy=\"localhost\""} {
		if strings.Contains(content, old) {
			t.Fatalf("old unmanaged proxy line remained:\n%s", content)
		}
	}
	for _, kept := range []string{"export EDITOR=vim", "alias ll='ls -lah'"} {
		if !strings.Contains(content, kept) {
			t.Fatalf("unrelated content %q was removed:\n%s", kept, content)
		}
	}
	if !strings.Contains(content, proxyBegin) || !strings.Contains(content, "http://127.0.0.1:8888") {
		t.Fatalf("new managed proxy block missing:\n%s", content)
	}
}

func TestProxyReplacementKeepsCompoundShellCommands(t *testing.T) {
	input := "export http_proxy=http://localhost:1234; echo keep-command\nexport HTTPS_PROXY=\"http://localhost:1234\" && keep-another-command\nexport no_proxy=localhost\n"
	want := "export http_proxy=http://localhost:1234; echo keep-command\nexport HTTPS_PROXY=\"http://localhost:1234\" && keep-another-command\n"
	if got := removeUnmanagedProxyLines(input); got != want {
		t.Fatalf("unrelated commands removed: %q", got)
	}
}

func TestUnmanagedProxyAssignmentsIgnoresManagedBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bashrc")
	input := `export http_proxy="http://user:secret@old.example:8080"
export no_proxy="localhost"

` + proxyBegin + `
export http_proxy="http://managed.example:8080"
` + proxyEnd + `
`
	if err := os.WriteFile(path, []byte(input), 0644); err != nil {
		t.Fatal(err)
	}

	assignments, err := unmanagedProxyAssignments(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(assignments) != 2 {
		t.Fatalf("expected 2 unmanaged proxy assignments, got %#v", assignments)
	}
	if assignments[0].name != "http_proxy" || assignments[0].value != "http://user:secret@old.example:8080" {
		t.Fatalf("unexpected first assignment: %#v", assignments[0])
	}
	if assignments[1].name != "no_proxy" || assignments[1].value != "localhost" {
		t.Fatalf("unexpected second assignment: %#v", assignments[1])
	}
}

func TestMaskProxyValue(t *testing.T) {
	got := maskProxyValue("http://user:secret@example.com:8080")
	want := "http://user:******@example.com:8080"
	if got != want {
		t.Fatalf("maskProxyValue() = %q, want %q", got, want)
	}

	plain := "localhost,127.0.0.1"
	if got := maskProxyValue(plain); got != plain {
		t.Fatalf("maskProxyValue() = %q, want %q", got, plain)
	}
}

func TestNormalizeProxy(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "plain", raw: "127.0.0.1:8888", want: "http://127.0.0.1:8888"},
		{name: "ipv4", raw: "203.0.113.10:8080", want: "http://203.0.113.10:8080"},
		{name: "auth", raw: "admin:secret@192.168.1.1:8888", want: "http://admin:secret@192.168.1.1:8888"},
		{name: "url", raw: " http://203.0.113.10:8080/ ", want: "http://203.0.113.10:8080"},
		{name: "hostname", raw: "Proxy.Example.COM:07890", want: "http://proxy.example.com:7890"},
		{name: "https", raw: "https://proxy.example:443/", want: "https://proxy.example:443"},
		{name: "ipv6", raw: "[2001:db8::1]:7890", want: "http://[2001:db8::1]:7890"},
		{name: "ipv6 auth", raw: "http://user:secret@[::1]:7890/", want: "http://user:secret@[::1]:7890"},
		{name: "encoded credentials", raw: "user:pa%40ss%23%25@proxy:7890", want: "http://user:pa%40ss%23%25@proxy:7890"},
		{name: "encoded space", raw: "user:pa%20ss@proxy:7890", want: "http://user:pa%20ss@proxy:7890"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeProxy(tt.raw)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("normalizeProxy() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeProxyRejectsInvalidInput(t *testing.T) {
	for _, raw := range []string{
		"", "127.0.0.1", "127.0.0.1:0", "127.0.0.1:70000",
		"203.0.113.10\xff:8080",
		"203.0.113.10\xef\xbf:8080",
		"203.0.113.10\ufffd:8080",
		"admin:secret@203.0.113.10\xff:8080",
		"admin:secret@203.0.113.10\ufffd:8080",
		"admin:sec\xffret@203.0.113.10:8080",
		"admin:sec\ufffdret@203.0.113.10:8080",
		"proxy:8080/path", "proxy:8080?x=1", "proxy:8080?", "proxy:8080#fragment", "proxy:8080#",
		"proxy:8080/%2F", "socks5://proxy:1080", "http://proxy", "http://proxy:", "proxy:+8080",
		"192.168.999.1:8080", "192.168.1:8080", "-proxy:8080", "proxy-:8080", "proxy..example:8080",
		"proxy example:8080", "proxy\texample:8080", "proxy\u200b:8080", "proxy\ufeff:8080",
		"$(false):8080", "proxy\";false:8080", "http://proxy:8080\n", "http://proxy:8080\r",
		"http://user:secret%00@proxy:8080", "http://user:secret%09@proxy:8080", "http://user:secret%0a@proxy:8080",
		"http://user:secret%FF@proxy:8080", "http://user:secret%EF%BF%BD@proxy:8080", "http://user:secret%E2%80%8B@proxy:8080",
		"http://user:@proxy:8080", "http://:secret@proxy:8080", "http://user@proxy:8080", "http://[fe80::1%25eth0]:8080",
	} {
		if _, err := normalizeProxy(raw); err == nil {
			t.Fatalf("normalizeProxy(%q) did not return an error", raw)
		}
	}
}

func TestProxyCredentialsSurviveShellWriteReadAndSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bashrc")
	if err := os.WriteFile(path, []byte("# preserve\n"), 0600); err != nil {
		t.Fatal(err)
	}
	proxyURL := "http://user:pa$SNAIL_PROXY_TEST@proxy.example:8080"
	if err := writeProxyBlockReplacingUnmanaged(path, proxyURL); err != nil {
		t.Fatal(err)
	}
	if got, ok := proxyURLFromShellContent(readTestFile(t, path)); !ok || got != proxyURL {
		t.Fatalf("shell configuration changed credentials: %q, %v", got, ok)
	}
	cmd := exec.Command("bash", "--noprofile", "--norc", "-c", `source "$1"; printf '%s' "$HTTP_PROXY"`, "proxy-test", path)
	cmd.Env = append(os.Environ(), "SNAIL_PROXY_TEST=must-not-expand")
	output, err := cmd.Output()
	if err != nil || string(output) != proxyURL {
		t.Fatalf("sourcing configuration changed credentials: %q, %v", output, err)
	}
}

func TestInvalidProxyCannotChangeShellConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bashrc")
	original := "export EDITOR=vim\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"http://proxy\ufffd:8080", "http://proxy:8080/path", "http://user:secret%0a@proxy:8080"} {
		if err := writeProxyBlockReplacingUnmanaged(path, raw); err == nil {
			t.Fatalf("invalid proxy accepted: %q", raw)
		}
		if readTestFile(t, path) != original {
			t.Fatal("invalid input changed existing shell configuration")
		}
	}
}

func TestProxyErrorsAndMasksDoNotExposePasswords(t *testing.T) {
	for _, raw := range []string{"https://admin:private-secret@proxy:8080/", "http://admin:private-secret@[::1]:8080/", "http://admin:private-secret@proxy:bad", "http://admin:private-secret%zz@proxy:8080"} {
		if got := MaskProxyURL(raw); strings.Contains(got, "private-secret") || !strings.Contains(got, "******") {
			t.Fatalf("password exposed by mask: %q", got)
		}
		if _, err := NormalizeProxy(raw); err != nil && strings.Contains(err.Error(), "private-secret") {
			t.Fatalf("password exposed by validation: %v", err)
		}
	}
}

func TestCurrentProxyURL(t *testing.T) {
	clearProxyEnv(t)

	home := t.TempDir()
	account := &system.Account{Name: "test", Home: home}
	bashrc := filepath.Join(home, ".bashrc")

	if err := os.WriteFile(bashrc, []byte(proxyBegin+"\nexport http_proxy=\"http://admin:secret@192.168.1.1:8888\"\n"+proxyEnd+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got, ok := CurrentProxyURL(account)
	if !ok {
		t.Fatal("expected current proxy URL")
	}
	want := "http://admin:secret@192.168.1.1:8888"
	if got != want {
		t.Fatalf("CurrentProxyURL() = %q, want %q", got, want)
	}
}
