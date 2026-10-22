package firewall

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/shared"
)

func TestEditMenuChecksSavedFileAndLoadsOnlyAfterConfirmation(t *testing.T) {
	for _, answer := range []string{"y", "n"} {
		t.Run(answer, func(t *testing.T) {
			setupTest(t)
			commandExists = func(name string) bool { return name == "nft" || name == "vim" }
			var operations []string
			commandRun = func(name string, args ...string) error {
				if name != "vim" || !reflect.DeepEqual(args, []string{configPath}) {
					t.Fatalf("unexpected command: %s %v", name, args)
				}
				operations = append(operations, "edit")
				return os.WriteFile(configPath, []byte("table inet snail {}\n"), 0644)
			}
			commandOutput = func(name string, args ...string) (string, error) {
				if name == "nft" && reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
					return `{"nftables":[]}`, nil
				}
				if name != "nft" {
					t.Fatalf("unexpected command: %s %v", name, args)
				}
				switch {
				case reflect.DeepEqual(args, []string{"-c", "-f", configPath}):
					if shared.ReadFileString(configPath) != "table inet snail {}\n" {
						t.Fatal("must validate the saved edit")
					}
					operations = append(operations, "validate")
				case reflect.DeepEqual(args, []string{"-f", configPath}):
					operations = append(operations, "load")
				default:
					t.Fatalf("unexpected query: %s %v", name, args)
				}
				return "", nil
			}
			if err := Run(inputUI(t, "5\n"+answer+"\n\nq\n")); !errors.Is(err, shared.ErrReturnToMenu) {
				t.Fatal(err)
			}
			want := []string{"edit", "validate"}
			if answer == "y" {
				want = append(want, "load")
			}
			if !reflect.DeepEqual(operations, want) {
				t.Fatalf("operations=%v want=%v", operations, want)
			}
			if shared.ReadFileString(configPath) != "table inet snail {}\n" {
				t.Fatal("edited file must be retained regardless of loading choice")
			}
		})
	}
}

func TestMenuReflectsAutostartChangeAndQueriesStateOncePerDisplay(t *testing.T) {
	setupTest(t)
	commandExists = func(string) bool { return true }
	content, _ := buildConfig([]int{2222}, false, false)
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	state, commands, queries := "disabled", 0, 0
	commandOutput = func(name string, args ...string) (string, error) {
		if name == "systemctl" {
			queries++
			return autostartProperties(state), nil
		}
		if name == "nft" && reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
			return `{"nftables":[]}`, nil
		}
		if name == "nft" && reflect.DeepEqual(args, []string{"-c", "-f", configPath}) {
			return "", nil
		}
		if name == "unshare" {
			return "", errors.New("network namespace unavailable")
		}
		t.Fatalf("unexpected command: %s %v", name, args)
		return "", nil
	}
	commandRun = func(name string, args ...string) error {
		if name != "systemctl" || !reflect.DeepEqual(args, []string{"enable", nftablesUnit}) {
			t.Fatalf("unexpected mutation: %s %v", name, args)
		}
		commands++
		state = "enabled"
		return nil
	}
	output := capturePortOutput(t, func() {
		if err := Run(inputUI(t, "7\n1\ny\n\nq\nq\n")); !errors.Is(err, shared.ErrReturnToMenu) {
			t.Fatal(err)
		}
	})
	if commands != 1 || queries != 7 {
		t.Fatalf("commands=%d queries=%d; expected four menu snapshots and three toggle checks", commands, queries)
	}
	if !strings.Contains(output, "开启开机自启") || !strings.Contains(output, "关闭开机自启") {
		t.Fatal("menu did not switch the displayed next action after enabling")
	}
}

func TestMenuEmptyInputRefreshesAndUppercaseReloadWorks(t *testing.T) {
	setupTest(t)
	if err := os.WriteFile(configPath, []byte("table inet snail {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var operations []string
	commandOutput = func(name string, args ...string) (string, error) {
		if name != "nft" {
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		if reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
			return `{"nftables":[]}`, nil
		}
		operations = append(operations, strings.Join(args, " "))
		return "", nil
	}
	if err := Run(inputUI(t, "\nR\ny\n\nq\n")); !errors.Is(err, shared.ErrReturnToMenu) {
		t.Fatal(err)
	}
	want := []string{"-c -f " + configPath, "-f " + configPath}
	if !reflect.DeepEqual(operations, want) {
		t.Fatalf("empty input consumed an action or R failed: %v", operations)
	}
}
