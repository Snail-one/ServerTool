package security

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBuildSSHDConfigIncludesManagedMarkers(t *testing.T) {
	content := buildSSHDConfig(2222, "no")
	if !strings.Contains(content, managedSSHDConfigBegin) {
		t.Fatalf("managed begin marker missing:\n%s", content)
	}
	if !strings.Contains(content, managedSSHDConfigEnd) {
		t.Fatalf("managed end marker missing:\n%s", content)
	}
}

func TestIsManagedSSHDConfigRequiresCurrentMarkers(t *testing.T) {
	if !isManagedSSHDConfig(managedSSHDConfigBegin + "\nPort 2222\n" + managedSSHDConfigEnd + "\n") {
		t.Fatal("expected current managed marker to be recognized")
	}
	if isManagedSSHDConfig("# Managed by setup tool\nPort 2222\n") {
		t.Fatal("expected legacy managed marker to be unmanaged")
	}
	if isManagedSSHDConfig(managedSSHDConfigBegin + "\nPort 2222\n") {
		t.Fatal("expected config without end marker to be unmanaged")
	}
	if isManagedSSHDConfig("Port 2222\n") {
		t.Fatal("expected unmarked config to be unmanaged")
	}
}

func TestParseSSHDSettings(t *testing.T) {
	output := `
# ignored
port 2222
listenaddress 0.0.0.0:2222
listenaddress [::]:2222
authorizedkeysfile .ssh/authorized_keys .ssh/authorized_keys2
`

	settings := parseSSHDSettings(output)
	if got := firstSSHDSetting(settings, "Port"); got != "2222" {
		t.Fatalf("Port = %q, want 2222", got)
	}

	wantListen := []string{"0.0.0.0:2222", "[::]:2222"}
	if !reflect.DeepEqual(settings["listenaddress"], wantListen) {
		t.Fatalf("ListenAddress = %#v, want %#v", settings["listenaddress"], wantListen)
	}

	wantKeys := ".ssh/authorized_keys .ssh/authorized_keys2"
	if got := firstSSHDSetting(settings, "AuthorizedKeysFile"); got != wantKeys {
		t.Fatalf("AuthorizedKeysFile = %q, want %q", got, wantKeys)
	}
}

func TestBuildSSHSecurityRowsClassifiesCommonSettings(t *testing.T) {
	settings := parseSSHDSettings(`
port 2222
pubkeyauthentication yes
passwordauthentication no
kbdinteractiveauthentication yes
permitrootlogin prohibit-password
permitemptypasswords no
permituserenvironment yes
usepam yes
`)

	assertSSHDRow(t, settings, "SSH 端口", "2222", "")
	assertSSHDRow(t, settings, "密钥登录", "已开启", "安全")
	assertSSHDRow(t, settings, "密码登录", "已禁用", "安全")
	assertSSHDRow(t, settings, "键盘交互认证", "已开启", "需确认")
	assertSSHDRow(t, settings, "Root 登录", "仅允许密钥登录", "安全")
	assertSSHDRow(t, settings, "空密码登录", "已禁止", "安全")
	assertSSHDRow(t, settings, "用户环境变量", "已允许", "风险")
	assertSSHDRow(t, settings, "PAM 认证", "已开启", "信息")
}

func TestActiveSSHDPortDirectivesIgnoresComments(t *testing.T) {
	content := "#Port 22\n" +
		"Port 22\n" +
		"  PORT 2222 # custom\n" +
		"PasswordAuthentication no\n"

	directives := activeSSHDPortDirectives(content)
	if len(directives) != 2 {
		t.Fatalf("expected 2 active Port directives, got %#v", directives)
	}
	if directives[0].line != 2 || strings.TrimSpace(directives[0].text) != "Port 22" {
		t.Fatalf("unexpected first directive: %#v", directives[0])
	}
	if directives[1].line != 3 || strings.TrimSpace(directives[1].text) != "PORT 2222 # custom" {
		t.Fatalf("unexpected second directive: %#v", directives[1])
	}
}

func TestSSHDPortConfigPathsForIncludesConfigDirAndSkipsManagedCustomFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "sshd_config")
	configDir := filepath.Join(dir, "sshd_config.d")
	customPath := filepath.Join(configDir, "99-custom.conf")

	if err := os.Mkdir(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(configDir, "50-cloud-init.conf"),
		customPath,
		filepath.Join(configDir, "10-extra.conf"),
		filepath.Join(configDir, "ignored.txt"),
	} {
		if err := os.WriteFile(path, nil, 0644); err != nil {
			t.Fatal(err)
		}
	}

	got, err := sshdPortConfigPathsFor(configPath, configDir, customPath)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		configPath,
		filepath.Join(configDir, "10-extra.conf"),
		filepath.Join(configDir, "50-cloud-init.conf"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %#v, want %#v", got, want)
	}
}

func TestActiveSSHDPortDirectivesInFilesIncludesPath(t *testing.T) {
	dir := t.TempDir()
	mainConfig := filepath.Join(dir, "sshd_config")
	extraConfig := filepath.Join(dir, "50-cloud-init.conf")
	if err := os.WriteFile(mainConfig, []byte("Port 22\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(extraConfig, []byte("#Port 23\nPort 2222\n"), 0644); err != nil {
		t.Fatal(err)
	}

	directives, err := activeSSHDPortDirectivesInFiles([]string{mainConfig, extraConfig})
	if err != nil {
		t.Fatal(err)
	}
	if len(directives) != 2 {
		t.Fatalf("expected 2 directives, got %#v", directives)
	}
	if directives[0].path != mainConfig || directives[0].line != 1 {
		t.Fatalf("unexpected first directive: %#v", directives[0])
	}
	if directives[1].path != extraConfig || directives[1].line != 2 {
		t.Fatalf("unexpected second directive: %#v", directives[1])
	}
}

func TestCommentActiveSSHDPortDirectives(t *testing.T) {
	content := "#Port 22\n" +
		"Port 22\n" +
		"PasswordAuthentication no\r\n" +
		"\tPORT 2222 # custom\n"

	got, changed := commentActiveSSHDPortDirectives(content)
	if !changed {
		t.Fatal("expected active Port directives to be commented")
	}
	want := "#Port 22\n" +
		"# SNAIL disabled duplicate Port: Port 22\n" +
		"PasswordAuthentication no\r\n" +
		"# SNAIL disabled duplicate Port: \tPORT 2222 # custom\n"
	if got != want {
		t.Fatalf("commented content mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func assertSSHDRow(t *testing.T, settings sshdSettings, name, wantValue, wantStatus string) {
	t.Helper()
	for _, row := range buildSSHSecurityRows(settings) {
		if row.name != name {
			continue
		}
		if row.value != wantValue || row.status != wantStatus {
			t.Fatalf("%s row = value %q status %q, want value %q status %q", name, row.value, row.status, wantValue, wantStatus)
		}
		return
	}
	t.Fatalf("row %q not found", name)
}
