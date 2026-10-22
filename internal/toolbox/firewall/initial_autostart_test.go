package firewall

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/shared"
)

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
					content, _ = buildConfig([]int{22}, false, false)
				}
				if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
			}
			view := inputUI(t, "n\nn\n1\nnext\n")
			text := capturePortOutput(t, func() {
				if err := configure(view); err != nil {
					t.Fatal(err)
				}
			})
			want := [][]string{{"nft", "-f", configPath}}
			if !scenario.rebuild && scenario.state != "enabled" {
				want = append(want, []string{"systemctl", "enable", nftablesUnit})
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
			input := "n\nn\n1\n"
			if scenario == "cancel" {
				input = "n\nn\nq\n"
			}
			err := configure(inputUI(t, input))
			if err == nil {
				t.Fatal("failure was reported as success")
			}
			if scenario == "enable" || scenario == "unchanged result" || scenario == "verification" {
				if enableAttempts != 1 || !*loaded || !strings.Contains(err.Error(), "配置已写入并加载") || !strings.Contains(shared.ReadFileString(configPath), "define snail_ssh_port") {
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
		if err := configure(inputUI(t, "n\nn\n1\n")); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(text, "不支持自动设置") || !strings.Contains(text, "未自动设置开机自启") {
		t.Fatal("unsupported startup must not be reported as enabled")
	}
}
