package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestAuthorizedKeyEntriesMarksManagedKeys(t *testing.T) {
	content := "ssh-ed25519 AAAAmanual user@example\n\n" +
		sshAuthorizedKeysBegin + "\n" +
		"ssh-rsa AAAAmanaged managed@example\n" +
		sshAuthorizedKeysEnd + "\n"

	entries := authorizedKeyEntries(content)
	if len(entries) != 2 {
		t.Fatalf("expected 2 authorized key entries, got %#v", entries)
	}
	if entries[0].index != 1 || entries[0].managed || entries[0].line != "ssh-ed25519 AAAAmanual user@example" {
		t.Fatalf("unexpected manual entry: %#v", entries[0])
	}
	if entries[1].index != 2 || !entries[1].managed || entries[1].line != "ssh-rsa AAAAmanaged managed@example" {
		t.Fatalf("unexpected managed entry: %#v", entries[1])
	}
}

func TestParseAuthorizedKeySelection(t *testing.T) {
	got, err := parseAuthorizedKeySelection("2, 1 2", 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{2, 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selection mismatch: got %#v, want %#v", got, want)
	}

	if _, err := parseAuthorizedKeySelection("4", 3); err == nil {
		t.Fatal("expected out-of-range selection to fail")
	}
}

func TestRemoveAuthorizedKeyIndexesRemovesSelectedKeysOnly(t *testing.T) {
	content := "ssh-ed25519 AAAAmanual-one one@example\n" +
		sshAuthorizedKeysBegin + "\n" +
		"ssh-rsa AAAAmanaged managed@example\n" +
		sshAuthorizedKeysEnd + "\n" +
		"ssh-ed25519 AAAAmanual-two two@example\n"

	cleaned := removeAuthorizedKeyIndexes(content, map[int]struct{}{
		2: {},
		3: {},
	})

	if strings.Contains(cleaned, "AAAAmanaged") || strings.Contains(cleaned, "AAAAmanual-two") {
		t.Fatalf("selected keys remained:\n%s", cleaned)
	}
	if strings.Contains(cleaned, sshAuthorizedKeysBegin) || strings.Contains(cleaned, sshAuthorizedKeysEnd) {
		t.Fatalf("empty managed key block remained:\n%s", cleaned)
	}
	if !strings.Contains(cleaned, "AAAAmanual-one") {
		t.Fatalf("unselected key was removed:\n%s", cleaned)
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

	assertSSHDRow(t, settings, "SSH 端口", "2222", "已调整")
	assertSSHDRow(t, settings, "密钥登录", "已开启", "安全")
	assertSSHDRow(t, settings, "密码登录", "已禁用", "安全")
	assertSSHDRow(t, settings, "键盘交互认证", "已开启", "需确认")
	assertSSHDRow(t, settings, "Root 登录", "仅允许密钥登录", "安全")
	assertSSHDRow(t, settings, "空密码登录", "已禁止", "安全")
	assertSSHDRow(t, settings, "用户环境变量", "已允许", "风险")
	assertSSHDRow(t, settings, "PAM 认证", "已开启", "信息")
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
