package firewall

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/shared"
)

func autostartProperties(state string) string {
	return fmt.Sprintf("LoadState=loaded\nUnitFileState=%s\nExecStart={ path=/usr/sbin/nft ; argv[]=/usr/sbin/nft -f %s ; ignore_errors=no ; }\n", state, configPath)
}

func TestAutostartEnableAndDisableOnlyChangeBootSetting(t *testing.T) {
	for _, initial := range []string{"disabled", "enabled", "enabled-runtime"} {
		t.Run(initial, func(t *testing.T) {
			setupTest(t)
			commandExists = func(string) bool { return true }
			content, _ := buildConfig([]int{2222}, false, false)
			// Disabling must remain possible even when the config is missing.
			if initial != "enabled" {
				if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
			}
			state := initial
			var operations []string
			commandOutput = func(name string, args ...string) (string, error) {
				if name == "systemctl" {
					want := []string{"show", nftablesUnit, "--property=LoadState", "--property=UnitFileState", "--property=ExecStart"}
					if !reflect.DeepEqual(args, want) {
						t.Fatalf("unexpected service query: %v", args)
					}
					return autostartProperties(state), nil
				}
				if name == "nft" && reflect.DeepEqual(args, []string{"-c", "-f", configPath}) {
					operations = append(operations, "validate")
					return "", nil
				}
				t.Fatalf("unexpected command (must not load or flush rules): %s %v", name, args)
				return "", nil
			}
			commandRun = func(name string, args ...string) error {
				action := "enable"
				state = "enabled"
				if initial == "enabled" {
					action, state = "disable", "disabled"
				}
				if name != "systemctl" || !reflect.DeepEqual(args, []string{action, nftablesUnit}) {
					t.Fatalf("must only change startup, without --now: %s %v", name, args)
				}
				operations = append(operations, action)
				return nil
			}
			if err := toggleAutostart(inputUI(t, "y\n")); err != nil {
				t.Fatal(err)
			}
			want := []string{"validate", "enable"}
			if initial == "enabled" {
				want = []string{"disable"}
			}
			if !reflect.DeepEqual(operations, want) {
				t.Fatalf("operations=%v want=%v", operations, want)
			}
		})
	}
}

func TestAutostartStopsBeforeMutationOnUnsafeOrUnconfirmedChanges(t *testing.T) {
	for _, scenario := range []string{"nonroot", "no systemd", "query failure", "missing unit", "masked", "static", "different config", "missing config", "empty config", "comments only", "invalid config", "decline", "enter", "EOF", "file changed", "service changed"} {
		t.Run(scenario, func(t *testing.T) {
			setupTest(t)
			commandExists = func(string) bool { return scenario != "no systemd" }
			isRoot = func() bool { return scenario != "nonroot" }
			content, _ := buildConfig([]int{2222}, false, false)
			if scenario == "empty config" {
				content = " \n"
			} else if scenario == "comments only" {
				content = "#!/usr/sbin/nft -f\n# no rules\n"
			}
			if scenario != "missing config" {
				if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
			}
			queries := 0
			commandOutput = func(name string, args ...string) (string, error) {
				if name == "systemctl" {
					queries++
					properties := autostartProperties("disabled")
					switch scenario {
					case "query failure":
						return "not booted with systemd", errors.New("failed")
					case "missing unit":
						return "LoadState=not-found\nUnitFileState=\n", nil
					case "masked", "static":
						return autostartProperties(scenario), nil
					case "different config":
						return strings.ReplaceAll(properties, configPath, "/etc/sysconfig/nftables.conf"), nil
					case "service changed":
						if queries > 1 {
							return autostartProperties("enabled"), nil
						}
					case "file changed":
						if queries > 1 {
							if err := os.WriteFile(configPath, []byte("# changed\n"), 0644); err != nil {
								t.Fatal(err)
							}
						}
					}
					return properties, nil
				}
				if name != "nft" || !reflect.DeepEqual(args, []string{"-c", "-f", configPath}) {
					t.Fatalf("unexpected query: %s %v", name, args)
				}
				if scenario == "invalid config" {
					return "syntax error", errors.New("invalid config")
				}
				return "", nil
			}
			input := "y\n"
			if scenario == "decline" {
				input = "n\n"
			} else if scenario == "enter" {
				input = "\n"
			} else if scenario == "EOF" {
				input = ""
			}
			err := toggleAutostart(inputUI(t, input))
			if scenario == "decline" || scenario == "enter" {
				if !errors.Is(err, shared.ErrReturnToMenu) {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("unsafe or unsupported operation must fail")
			}
		})
	}
}

func TestAutostartVerifiesMutationResult(t *testing.T) {
	for _, failure := range []string{"command", "unchanged", "verification"} {
		t.Run(failure, func(t *testing.T) {
			setupTest(t)
			commandExists = func(string) bool { return true }
			mutated := false
			commandOutput = func(string, ...string) (string, error) {
				if mutated && failure == "verification" {
					return "", errors.New("cannot query service")
				}
				return autostartProperties("enabled"), nil
			}
			commandRun = func(string, ...string) error {
				mutated = true
				if failure == "command" {
					return errors.New("permission denied")
				}
				return nil
			}
			if err := toggleAutostart(inputUI(t, "y\n")); err == nil || !mutated {
				t.Fatalf("mutation failure ignored: mutated=%v err=%v", mutated, err)
			}
		})
	}
}

func TestAutostartStatusDistinguishesBootAndRuntimeOnly(t *testing.T) {
	setupTest(t)
	commandExists = func(string) bool { return true }
	for _, state := range []string{"enabled", "disabled", "enabled-runtime"} {
		commandOutput = func(string, ...string) (string, error) {
			return autostartProperties(state), nil
		}
		status := inspectAutostart()
		if status.err != nil || status.enabled != (state == "enabled") {
			t.Fatalf("state=%s status=%+v", state, status)
		}
	}
}

func TestCleanupDisablesAutostartOnlyWhenNoOtherConfigRemains(t *testing.T) {
	for _, otherConfig := range []bool{false, true} {
		t.Run(fmt.Sprintf("other config=%v", otherConfig), func(t *testing.T) {
			setupTest(t)
			commandExists = func(string) bool { return true }
			content, _ := buildConfig([]int{2222}, false, false)
			if otherConfig {
				content += "\ntable inet other {}\n"
			}
			if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			disabled := false
			commandOutput = func(name string, args ...string) (string, error) {
				if name == "nft" && reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
					return `{"nftables":[]}`, nil
				}
				if name == "systemctl" && !otherConfig {
					if disabled {
						return autostartProperties("disabled"), nil
					}
					return autostartProperties("enabled"), nil
				}
				t.Fatalf("unexpected query: %s %v", name, args)
				return "", nil
			}
			commandRun = func(name string, args ...string) error {
				if otherConfig || name != "systemctl" || !reflect.DeepEqual(args, []string{"disable", nftablesUnit}) {
					t.Fatalf("unexpected mutation: %s %v", name, args)
				}
				disabled = true
				return nil
			}
			if err := Cleanup(nil); err != nil {
				t.Fatal(err)
			}
			if disabled == otherConfig {
				t.Fatalf("disabled=%v otherConfig=%v", disabled, otherConfig)
			}
		})
	}
}
