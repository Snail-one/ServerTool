package clientproxy

import (
	"errors"
	"testing"

	"snail_tool/internal/shared"
)

type removalPrompt struct {
	confirm func() (bool, error)
}

func (p removalPrompt) Ask(string) (string, error)   { panic("unexpected proxy address prompt") }
func (p removalPrompt) Confirm(string) (bool, error) { return p.confirm() }

func TestRemoveClientProxyPreservesCredentialsAndCreatesBackup(t *testing.T) {
	account, path := setup(t)
	putFile(t, path, `{"auths":{"registry":{"auth":"keep"}},"proxies":{"default":{"ftpProxy":"ftp://keep:21"},"tcp://remote:2376":{"httpProxy":"http://remote:8080"}}}`)
	if _, err := writeConfig(path, account, "http://proxy:8080", nil); err != nil {
		t.Fatal(err)
	}
	original := shared.ReadFileString(path)
	if err := remove(removalPrompt{confirm: accept}, account); err != nil {
		t.Fatal(err)
	}
	doc, _, _, err := readDocument(path)
	if err != nil || doc.hasProxy() || string(compact(t, doc.root["auths"])) != `{"registry":{"auth":"keep"}}` || len(doc.defaults) != 1 || doc.proxies["tcp://remote:2376"] == nil {
		t.Fatalf("unrelated config changed: %#v, %v", doc, err)
	}
	entries, err := managedStore.List()
	if err != nil || len(entries) != 2 {
		t.Fatalf("missing removal backup: %v, %v", entries, err)
	}
	found := false
	for _, entry := range entries {
		found = found || shared.ReadFileString(entry.Path) == original
	}
	if !found {
		t.Fatal("pre-removal contents were not backed up")
	}
	if err := remove(removalPrompt{confirm: func() (bool, error) { t.Fatal("unexpected confirmation for absent proxy"); return false, nil }}, account); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveClientProxyCancellationConcurrentChangesAndBackupFailure(t *testing.T) {
	for _, scenario := range []string{"cancel", "return", "concurrent login", "backup failure"} {
		t.Run(scenario, func(t *testing.T) {
			account, path := setup(t)
			if _, err := writeConfig(path, account, "http://proxy:8080", nil); err != nil {
				t.Fatal(err)
			}
			want := shared.ReadFileString(path)
			prompt := removalPrompt{confirm: func() (bool, error) {
				switch scenario {
				case "cancel":
					return false, nil
				case "return":
					return false, shared.ErrReturnToMenu
				case "concurrent login":
					want += "\n"
					putFile(t, path, want)
				case "backup failure":
					managedStore.Root = path
				}
				return true, nil
			}}
			err := remove(prompt, account)
			if err == nil || shared.ReadFileString(path) != want {
				t.Fatalf("refused removal changed config: %v", err)
			}
			if (scenario == "cancel" || scenario == "return") && !errors.Is(err, shared.ErrReturnToMenu) {
				t.Fatalf("cancellation did not return to menu: %v", err)
			}
		})
	}
}

func TestRemoveClientProxyRefusesUnmanagedConfigWithoutPrompt(t *testing.T) {
	account, path := setup(t)
	original := `{"proxies":{"default":{"httpProxy":"http://external:8080"}}}`
	putFile(t, path, original)
	err := remove(removalPrompt{confirm: func() (bool, error) { t.Fatal("unexpected confirmation for unowned file"); return true, nil }}, account)
	if err == nil || shared.ReadFileString(path) != original {
		t.Fatalf("unowned proxy removed: %v", err)
	}
}
