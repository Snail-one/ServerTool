package daemonproxy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"snail_tool/internal/shared"
)

func TestBuildDockerProxyConfig(t *testing.T) {
	proxyURL := "http://admin:123456@192.168.1.100:7890/"
	got := buildDockerProxyConfig(proxyURL)
	want := `[Service]
Environment="HTTP_PROXY=http://admin:123456@192.168.1.100:7890/"
Environment="HTTPS_PROXY=http://admin:123456@192.168.1.100:7890/"
Environment="NO_PROXY=localhost,127.0.0.1,::1,.localhost,.local,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,169.254.0.0/16,fc00::/7,fe80::/10"
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
	previous := managedStore
	managedStore = shared.BackupStore{Root: filepath.Join(t.TempDir(), "snailtool"), Module: "docker"}
	t.Cleanup(func() { managedStore = previous })
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
		`Environment="NO_PROXY=localhost,127.0.0.1,::1,.localhost,.local,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,169.254.0.0/16,fc00::/7,fe80::/10"`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("missing %q in:\n%s", want, content)
		}
	}
}

func TestDockerEnvironmentValueFromContent(t *testing.T) {
	content := `[Service]
Environment="HTTP_PROXY=http://admin:secret@192.168.1.100:7890/"
Environment="HTTPS_PROXY=http://admin:secret@192.168.1.100:7890/"
Environment="NO_PROXY=localhost,127.0.0.1"
`
	got, ok := dockerEnvironmentValue(content, "HTTP_PROXY")
	want := "http://admin:secret@192.168.1.100:7890/"
	if !ok || got != want {
		t.Fatalf("dockerEnvironmentValue() = %q, %v; want %q, true", got, ok, want)
	}
}

func TestSummarizeProxyConfig(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name    string
		path    string
		content string
		want    string
	}{
		{name: "missing", path: filepath.Join(dir, "missing.conf"), want: "未配置"},
		{name: "empty", path: filepath.Join(dir, "empty.conf"), content: "\n", want: "未配置"},
		{
			name:    "masked",
			path:    filepath.Join(dir, "masked.conf"),
			content: "Environment=\"HTTP_PROXY=http://admin:secret@192.168.1.100:7890/\"\nEnvironment=\"HTTPS_PROXY=http://admin:secret@192.168.1.100:7890/\"\n",
			want:    "http://admin:******@192.168.1.100:7890/",
		},
		{
			name:    "different proxies",
			path:    filepath.Join(dir, "different.conf"),
			content: "Environment=\"HTTP_PROXY=http://127.0.0.1:7890/\"\nEnvironment=\"HTTPS_PROXY=http://10.0.0.8:7890/\"\n",
			want:    "HTTP http://127.0.0.1:7890/，HTTPS http://10.0.0.8:7890/",
		},
		{name: "unrecognized", path: filepath.Join(dir, "other.conf"), content: "[Service]\n", want: "未识别代理地址"},
		{name: "unreadable", path: dir, want: "无法读取"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.content != "" {
				if err := os.WriteFile(tt.path, []byte(tt.content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if got := summarizeProxyConfig(tt.path); got != tt.want {
				t.Fatalf("summarizeProxyConfig() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConfiguredDockerProxyURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "http-proxy.conf")
	if err := os.WriteFile(path, []byte(`Environment="HTTPS_PROXY=http://127.0.0.1:7890/"`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got, ok := dockerEnvironmentValue(shared.ReadFileString(path), "HTTPS_PROXY")
	want := "http://127.0.0.1:7890/"
	if !ok || got != want {
		t.Fatalf("dockerEnvironmentValue() = %q, %v; want %q, true", got, ok, want)
	}
}
