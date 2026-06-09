package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
}

func TestWriteProxyBlockIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bashrc")
	if err := os.WriteFile(path, []byte("export EDITOR=vim\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := writeProxyBlock(path, "http://127.0.0.1:8888"); err != nil {
		t.Fatal(err)
	}
	if err := writeProxyBlock(path, "http://admin:secret@192.168.1.1:8888"); err != nil {
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
	if !strings.Contains(content, "export EDITOR=vim") {
		t.Fatalf("unrelated bashrc content was removed:\n%s", content)
	}
}

func TestNormalizeProxy(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "plain", raw: "127.0.0.1:8888", want: "http://127.0.0.1:8888"},
		{name: "auth", raw: "admin:secret@192.168.1.1:8888", want: "http://admin:secret@192.168.1.1:8888"},
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
	for _, raw := range []string{"", "127.0.0.1", "127.0.0.1:0", "127.0.0.1:70000"} {
		if _, err := normalizeProxy(raw); err == nil {
			t.Fatalf("normalizeProxy(%q) did not return an error", raw)
		}
	}
}

func TestCurrentProxyURL(t *testing.T) {
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
