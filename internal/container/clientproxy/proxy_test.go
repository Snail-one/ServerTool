package clientproxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	commonproxy "snail_tool/internal/common/proxy"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
)

func setup(t *testing.T) (*system.Account, string) {
	t.Helper()
	t.Setenv("DOCKER_CONFIG", "")
	previous := managedStore
	managedStore = shared.BackupStore{Root: filepath.Join(t.TempDir(), "data"), Module: "docker"}
	t.Cleanup(func() { managedStore = previous })
	account := &system.Account{Name: "tester", Home: t.TempDir(), UID: os.Getuid(), GID: os.Getgid()}
	path, err := ConfigPath(account)
	if err != nil {
		t.Fatal(err)
	}
	return account, path
}

func putFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func accept() (bool, error) { return true, nil }

func TestConfigPathUsesTargetUserAndDockerConfig(t *testing.T) {
	account, path := setup(t)
	t.Setenv("HOME", "/root")
	if path != filepath.Join(account.Home, ".docker", "config.json") {
		t.Fatal(path)
	}
	for _, dir := range []string{t.TempDir(), "relative-docker-config"} {
		t.Setenv("DOCKER_CONFIG", dir)
		got, err := ConfigPath(account)
		want, _ := filepath.Abs(filepath.Join(dir, "config.json"))
		if err != nil || got != want {
			t.Fatalf("ConfigPath() = %s, %v; want %s", got, err, want)
		}
	}
}

func TestWriteAndCleanupPreserveCredentialsAndOtherProxySettings(t *testing.T) {
	account, path := setup(t)
	original := `{"auths":{"registry.example":{"auth":"keep-secret"}},"credsStore":"pass","number":9007199254740993,"proxies":{"default":{"httpProxy":"http://old:8080","ftpProxy":"ftp://keep:21","allProxy":"socks5://keep:1080"},"tcp://remote:2376":{"httpProxy":"http://remote:8080"}}}`
	putFile(t, path, original)
	proxyURL := "http://admin:secret@192.168.1.100:7890"
	backup, err := writeConfig(path, account, proxyURL, accept)
	if err != nil {
		t.Fatal(err)
	}
	if got := shared.ReadFileString(backup); got != original {
		t.Fatalf("backup does not match original: %s", got)
	}
	for _, file := range []string{path, backup} {
		info, err := os.Stat(file)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("private permissions: %s, %v, %v", file, info, err)
		}
	}
	doc, _, _, err := readDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"httpProxy": proxyURL, "httpsProxy": proxyURL, "noProxy": commonproxy.DefaultNoProxy()} {
		var got string
		if err := json.Unmarshal(doc.defaults[key], &got); err != nil || got != want {
			t.Fatalf("%s = %s, %v", key, got, err)
		}
	}
	if summary := doc.summary(); strings.Contains(summary, "secret") || !strings.Contains(summary, "192.168.1.100:7890") {
		t.Fatalf("unmasked or missing proxy: %s", summary)
	}
	if err := Cleanup(account); err != nil {
		t.Fatal(err)
	}
	cleaned, data, _, err := readDocument(path)
	if err != nil || cleaned.hasProxy() {
		t.Fatalf("default proxy remains: %s, %v", data, err)
	}
	var got, want map[string]json.RawMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(original), &want); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"auths", "credsStore", "number"} {
		if !bytes.Equal(compact(t, got[key]), compact(t, want[key])) {
			t.Fatalf("unrelated %s changed: %s", key, got[key])
		}
	}
	if !bytes.Equal(compact(t, cleaned.proxies["tcp://remote:2376"]), []byte(`{"httpProxy":"http://remote:8080"}`)) || len(cleaned.defaults) != 2 {
		t.Fatalf("other proxy settings changed: %s", data)
	}
	if string(cleaned.defaults["ftpProxy"]) != `"ftp://keep:21"` || string(cleaned.defaults["allProxy"]) != `"socks5://keep:1080"` {
		t.Fatalf("other default proxies changed: %s", data)
	}
	if err := Cleanup(account); err != nil || !bytes.Equal(data, []byte(shared.ReadFileString(path))) {
		t.Fatalf("repeated cleanup changed config: %v", err)
	}
	if matches, err := managedStore.MatchesManaged(path, data); err != nil || matches {
		t.Fatalf("managed record remains: %v, %v", matches, err)
	}
}

func compact(t *testing.T, raw []byte) []byte {
	t.Helper()
	var result bytes.Buffer
	if err := json.Compact(&result, raw); err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}

func TestNewConfigCreatesPrivateDirectoryAndCleansToEmptyObject(t *testing.T) {
	account, path := setup(t)
	if backup, err := writeConfig(path, account, "http://proxy:8080", nil); err != nil || backup != "" {
		t.Fatalf("new config: %s, %v", backup, err)
	}
	info, err := os.Stat(filepath.Dir(path))
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("private directory: %v, %v", info, err)
	}
	if err := Cleanup(account); err != nil {
		t.Fatal(err)
	}
	if got := shared.ReadFileString(path); got != "{}\n" {
		t.Fatalf("cleaned config = %s", got)
	}
}

func TestWriteRefusesMalformedConfigAndCanceledOverwrite(t *testing.T) {
	for _, original := range []string{"", "null", "[]", "{}{}", `{"proxies":null}`, `{"proxies":[]}`, `{"proxies":{"default":null}}`, `{"proxies":{"default":"invalid"}}`, `{"proxies":{"default":{"noProxy":"keep"}}}`} {
		t.Run(original, func(t *testing.T) {
			account, path := setup(t)
			putFile(t, path, original)
			_, err := writeConfig(path, account, "http://proxy:8080", func() (bool, error) { return false, nil })
			if err == nil || shared.ReadFileString(path) != original {
				t.Fatalf("invalid or canceled config was overwritten: %v", err)
			}
			entries, err := managedStore.List()
			if err != nil || len(entries) != 0 {
				t.Fatalf("unexpected backup on refusal: %v, %v", entries, err)
			}
		})
	}
}

func TestWriteAndCleanupRefuseSymlinks(t *testing.T) {
	for _, ancestor := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "directory"}[ancestor], func(t *testing.T) {
			account, path := setup(t)
			targetDir := t.TempDir()
			target := filepath.Join(targetDir, "config.json")
			putFile(t, target, `{"auths":{"keep":{}}}`)
			link, dest := filepath.Dir(path), targetDir
			if !ancestor {
				if err := os.Mkdir(link, 0700); err != nil {
					t.Fatal(err)
				}
				link, dest = path, target
			}
			if err := os.Symlink(dest, link); err != nil {
				t.Fatal(err)
			}
			if _, err := writeConfig(path, account, "http://proxy:8080", accept); err == nil {
				t.Fatal("symlink write accepted")
			}
			if err := Cleanup(account); err == nil {
				t.Fatal("symlink cleanup accepted")
			}
			if shared.ReadFileString(target) != `{"auths":{"keep":{}}}` {
				t.Fatal("symlink target changed")
			}
		})
	}
}

func TestCleanupRefusesUnownedOrModifiedConfig(t *testing.T) {
	for _, modified := range []bool{false, true} {
		t.Run(map[bool]string{false: "unowned", true: "modified"}[modified], func(t *testing.T) {
			account, path := setup(t)
			if modified {
				if _, err := writeConfig(path, account, "http://proxy:8080", nil); err != nil {
					t.Fatal(err)
				}
				putFile(t, path, shared.ReadFileString(path)+"\n")
			} else {
				putFile(t, path, `{"proxies":{"default":{"httpProxy":"http://external:8080"}}}`)
			}
			original := shared.ReadFileString(path)
			if err := Cleanup(account); err == nil || shared.ReadFileString(path) != original {
				t.Fatalf("ambiguous cleanup changed config: %v", err)
			}
		})
	}
}

type fakePrompt struct {
	answer string
	ok     bool
}

func (p fakePrompt) Ask(string) (string, error)   { return p.answer, nil }
func (p fakePrompt) Confirm(string) (bool, error) { return p.ok, nil }

func TestConfigureReturnAndOverwriteCancellationLeaveConfigUntouched(t *testing.T) {
	account, path := setup(t)
	for _, answer := range []string{"q", "192.168.1.100:7890"} {
		original := `{"proxies":{"default":{"httpProxy":"http://keep:8080"}}}`
		putFile(t, path, original)
		if err := configure(fakePrompt{answer: answer}, account); !errors.Is(err, shared.ErrReturnToMenu) {
			t.Fatalf("expected return to menu, got %v", err)
		}
		if shared.ReadFileString(path) != original {
			t.Fatal("canceled config changed")
		}
	}
}

func TestConfigureRejectsCorruptedAddressBeforeWriting(t *testing.T) {
	for _, answer := range []string{"203.0.113.10\xff:8080", "203.0.113.10\ufffd:8080"} {
		t.Run(answer, func(t *testing.T) {
			account, path := setup(t)
			original := `{"auths":{"registry":{"auth":"keep"}},"proxies":{"default":{"httpProxy":"http://keep:8080"}}}`
			putFile(t, path, original)
			err := configure(fakePrompt{answer: answer, ok: true}, account)
			if err == nil || shared.ReadFileString(path) != original {
				t.Fatalf("corrupted input was not rejected without changes: %v", err)
			}
			entries, err := managedStore.List()
			if err != nil || len(entries) != 0 {
				t.Fatalf("unexpected backup on invalid input: %v, %v", entries, err)
			}
		})
	}
}

func TestClientProxyWriterValidatesBeforeBackupOrWrite(t *testing.T) {
	account, path := setup(t)
	original := `{"auths":{"registry":{"auth":"keep"}}}`
	putFile(t, path, original)
	for _, raw := range []string{"http://proxy\ufffd:8080", "http://proxy:8080/path", "http://user:secret%0a@proxy:8080"} {
		backup, err := writeConfig(path, account, raw, accept)
		if err == nil || backup != "" || shared.ReadFileString(path) != original {
			t.Fatalf("invalid input changed or backed up configuration: %q, %v", backup, err)
		}
	}
	if entries, err := managedStore.List(); err != nil || len(entries) != 0 {
		t.Fatalf("invalid input created persistent records: %v, %v", entries, err)
	}
}

func TestConcurrentChangeDuringOverwriteConfirmationIsPreserved(t *testing.T) {
	account, path := setup(t)
	putFile(t, path, `{"proxies":{"default":{"httpProxy":"http://old:8080"}}}`)
	changed := `{"auths":{"new-registry":{"auth":"new-login"}},"proxies":{"default":{"httpProxy":"http://old:8080"}}}`
	_, err := writeConfig(path, account, "http://proxy:8080", func() (bool, error) {
		putFile(t, path, changed)
		return true, nil
	})
	if err == nil || shared.ReadFileString(path) != changed {
		t.Fatalf("concurrent login changes were lost: %v", err)
	}
}

func TestBackupFailureLeavesExistingConfigUntouched(t *testing.T) {
	account, path := setup(t)
	original := `{"auths":{"registry":{"auth":"keep"}}}`
	putFile(t, path, original)
	rootFile := filepath.Join(t.TempDir(), "not-a-directory")
	putFile(t, rootFile, "file")
	managedStore.Root = rootFile
	if _, err := writeConfig(path, account, "http://proxy:8080", nil); err == nil || shared.ReadFileString(path) != original {
		t.Fatalf("config changed despite backup failure: %v", err)
	}
}
