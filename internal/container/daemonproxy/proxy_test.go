package daemonproxy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildDockerProxyConfig(t *testing.T) {
	proxyURL := "http://admin:123456@192.168.1.100:7890/"
	got := buildDockerProxyConfig(proxyURL)
	want := `[Service]
Environment="HTTP_PROXY=http://admin:123456@192.168.1.100:7890/"
Environment="HTTPS_PROXY=http://admin:123456@192.168.1.100:7890/"
Environment="NO_PROXY=localhost,127.0.0.1,127.0.0.0/8,::1,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,169.254.0.0/16,*.local"
`
	if got != want {
		t.Fatalf("buildDockerProxyConfig() = %q, want %q", got, want)
	}
}

func TestDockerProxyURLAddsTrailingSlash(t *testing.T) {
	got := dockerProxyURL("http://192.168.1.100:7890")
	want := "http://192.168.1.100:7890/"
	if got != want {
		t.Fatalf("dockerProxyURL() = %q, want %q", got, want)
	}

	got = dockerProxyURL("http://192.168.1.100:7890/")
	if got != want {
		t.Fatalf("dockerProxyURL() = %q, want %q", got, want)
	}
}

func TestWriteDockerProxyConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "docker.service.d", "http-proxy.conf")
	proxyURL := "http://127.0.0.1:7890/"
	if err := writeDockerProxyConfig(path, proxyURL); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{
		`Environment="HTTP_PROXY=http://127.0.0.1:7890/"`,
		`Environment="HTTPS_PROXY=http://127.0.0.1:7890/"`,
		`Environment="NO_PROXY=` + dockerNoProxy + `"`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("missing %q in:\n%s", want, content)
		}
	}
}

func TestDockerProxyURLFromContent(t *testing.T) {
	content := `[Service]
Environment="HTTP_PROXY=http://admin:secret@192.168.1.100:7890/"
Environment="HTTPS_PROXY=http://admin:secret@192.168.1.100:7890/"
Environment="NO_PROXY=localhost,127.0.0.1"
`
	got, ok := dockerProxyURLFromContent(content)
	want := "http://admin:secret@192.168.1.100:7890/"
	if !ok || got != want {
		t.Fatalf("dockerProxyURLFromContent() = %q, %v; want %q, true", got, ok, want)
	}
}

func TestConfiguredDockerProxyURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "http-proxy.conf")
	if err := os.WriteFile(path, []byte(`Environment="HTTPS_PROXY=http://127.0.0.1:7890/"`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got, ok := configuredDockerProxyURL(path)
	want := "http://127.0.0.1:7890/"
	if !ok || got != want {
		t.Fatalf("configuredDockerProxyURL() = %q, %v; want %q, true", got, ok, want)
	}
}
