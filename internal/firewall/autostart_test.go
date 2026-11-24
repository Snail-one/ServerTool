package firewall

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/firewall/configuration/generate"
	autostartconfig "snail_tool/internal/firewall/settings/autostart"
	"snail_tool/internal/storage"
	"snail_tool/internal/ui"
)

func autostartProperties(state string) string {
	return fmt.Sprintf("LoadState=loaded\nUnitFileState=%s\nExecStart={ path=/usr/sbin/nft ; argv[]=/usr/sbin/nft -f %s ; ignore_errors=no ; }\n", state, configPath)
}

func TestAutostartEnableAndDisableOnlyChangeBootSetting(t *testing.T) {
	for _, initial := range []string{"disabled", "enabled", "enabled-runtime"} {
		t.Run(initial, func(t *testing.T) {
			setupTest(t)
			commandExists = func(string) bool { return true }
			content, _ := generate.Build([]int{2222}, false, false)
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
					want := []string{"show", autostartconfig.Unit, "--property=LoadState", "--property=UnitFileState", "--property=ExecStart"}
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
				if name != "systemctl" || !reflect.DeepEqual(args, []string{action, autostartconfig.Unit}) {
					t.Fatalf("must only change startup, without --now: %s %v", name, args)
				}
				operations = append(operations, action)
				return nil
			}
			if err := autostartService().Toggle(inputUI(t, "y\n")); err != nil {
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
			content, _ := generate.Build([]int{2222}, false, false)
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
			err := autostartService().Toggle(inputUI(t, input))
			if scenario == "decline" || scenario == "enter" {
				if !errors.Is(err, ui.ErrReturnToMenu) {
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
			if err := autostartService().Toggle(inputUI(t, "y\n")); err == nil || !mutated {
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
		status := autostartService().Inspect()
		if status.Err != nil || status.Enabled != (state == "enabled") {
			t.Fatalf("state=%s status=%+v", state, status)
		}
	}
}

func TestCleanupDisablesAutostartOnlyWhenNoOtherConfigRemains(t *testing.T) {
	for _, otherConfig := range []bool{false, true} {
		t.Run(fmt.Sprintf("other config=%v", otherConfig), func(t *testing.T) {
			setupTest(t)
			commandExists = func(string) bool { return true }
			content, _ := generate.Build([]int{2222}, false, false)
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
				if otherConfig || name != "systemctl" || !reflect.DeepEqual(args, []string{"disable", autostartconfig.Unit}) {
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

func TestFirstConfigurationEnablesAutostartAfterLoadAndRebuildPreservesIt(t *testing.T) {
	for _, scenario := range []struct {
		name, state string
		existing    bool
		rebuild     bool
	}{
		{"new", "disabled", false, false},
		{"replace vendor config", "disabled", true, false},
		{"already enabled", "enabled", false, false},
		{"runtime to persistent", "enabled-runtime", false, false},
		{"rebuild stays disabled", "disabled", true, true},
		{"rebuild stays enabled", "enabled", true, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			state, loaded, commands := originalFixture(t, scenario.state)
			output := commandOutput
			commandOutput = func(name string, args ...string) (string, error) {
				if isSSHDTest(name, args) {
					return sshdTestConfig(2222), nil
				}
				return output(name, args...)
			}
			if scenario.existing {
				content := "# distribution configuration\n"
				if scenario.rebuild {
					content, _ = generate.Build([]int{22}, false, false)
				}
				if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
			}
			view := inputUI(t, "\n\n\n\n1\nnext\n")
			text := capturePortOutput(t, func() {
				if err := configure(view); err != nil {
					t.Fatal(err)
				}
			})
			want := [][]string{{"nft", "-f", configPath}}
			if !scenario.rebuild && scenario.state != "enabled" {
				want = append(want, []string{"systemctl", "enable", autostartconfig.Unit})
			}
			if !*loaded || !reflect.DeepEqual(*commands, want) {
				t.Fatalf("must load before enabling, without --now: %v", *commands)
			}
			if scenario.rebuild {
				if *state != scenario.state || !strings.Contains(text, "保持当前设置") {
					t.Fatal("rebuilding changed startup preference")
				}
			} else {
				first, err := readOriginalState()
				if *state != "enabled" || err != nil || first == nil || first.Autostart != scenario.state {
					t.Fatalf("initial startup state not preserved before enabling: %+v %v", first, err)
				}
				if !strings.Contains(text, "首次配置加载成功后自动开启") {
					t.Fatal("confirmation did not disclose automatic startup")
				}
			}
			if next, err := view.Ask("next"); err != nil || next != "next" {
				t.Fatal("initial setup consumed an extra confirmation")
			}
		})
	}
}

func TestInitialAutostartDoesNotEnableAfterAbortOrFailure(t *testing.T) {
	for _, scenario := range []string{"cancel", "wrong config", "masked", "service changed", "validation", "load", "enable", "unchanged result", "verification"} {
		t.Run(scenario, func(t *testing.T) {
			_, loaded, commands := originalFixture(t, "disabled")
			output, run := commandOutput, commandRun
			queries, enableAttempts := 0, 0
			commandOutput = func(name string, args ...string) (string, error) {
				if isSSHDTest(name, args) {
					return sshdTestConfig(2222), nil
				}
				if name == "systemctl" {
					queries++
					if scenario == "wrong config" {
						return strings.ReplaceAll(autostartProperties("disabled"), configPath, "/etc/other.conf"), nil
					}
					if scenario == "masked" {
						return autostartProperties("masked"), nil
					}
					if scenario == "service changed" && queries > 1 {
						return autostartProperties("enabled"), nil
					}
					if scenario == "verification" && enableAttempts > 0 {
						return "", errors.New("status unavailable")
					}
				}
				if name == "nft" && len(args) == 3 && args[0] == "-c" && scenario == "validation" {
					return "syntax error", errors.New("validation failed")
				}
				if name == "nft" && reflect.DeepEqual(args, []string{"-f", configPath}) && scenario == "load" {
					return "load failed", errors.New("load failed")
				}
				return output(name, args...)
			}
			commandRun = func(name string, args ...string) error {
				enableAttempts++
				if scenario == "enable" {
					return errors.New("enable failed")
				}
				if scenario == "unchanged result" {
					return nil
				}
				return run(name, args...)
			}
			input := "\n\n\n\n1\n"
			if scenario == "cancel" {
				input = "\n\n\n\nq\n"
			}
			err := configure(inputUI(t, input))
			if err == nil {
				t.Fatal("failure was reported as success")
			}
			if scenario == "enable" || scenario == "unchanged result" || scenario == "verification" {
				if enableAttempts != 1 || !*loaded || !strings.Contains(err.Error(), "配置已写入并加载") || !strings.Contains(storage.ReadFileString(configPath), "set snail_ssh_port") {
					t.Fatalf("startup failure must preserve and report the active configuration: %v", err)
				}
			} else if enableAttempts != 0 || *loaded || len(*commands) != 0 {
				t.Fatalf("aborted setup changed the service: %v %v", *commands, err)
			}
		})
	}
}

func TestFirstConfigurationWithoutSystemdReportsUnsupportedStartup(t *testing.T) {
	setupTest(t)
	acceptRuleCommands(t)
	output := commandOutput
	commandOutput = func(name string, args ...string) (string, error) {
		if isSSHDTest(name, args) {
			return sshdTestConfig(2222), nil
		}
		return output(name, args...)
	}
	text := capturePortOutput(t, func() {
		if err := configure(inputUI(t, "\n\n\n\n1\n")); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(text, "不支持自动设置") || !strings.Contains(text, "未自动设置开机自启") {
		t.Fatal("unsupported startup must not be reported as enabled")
	}
}
