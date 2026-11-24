package unattended

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"snail_tool/internal/ui"
)

const previousPolicy = managedHeader + `APT::Periodic::Update-Package-Lists "2";
APT::Periodic::Unattended-Upgrade "7";
Unattended-Upgrade::InstallOnShutdown "true";
Unattended-Upgrade::Origins-Pattern { "origin=Debian"; };
`

func TestManagementMenuShowsDistinctPolicyAndTimerStatus(t *testing.T) {
	t.Setenv("TERM", "dumb")
	t.Setenv("NO_COLOR", "1")
	file, err := os.CreateTemp(t.TempDir(), "menu-input")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString("3\nq\n4\nq\n6\n\nq\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	previousInput := os.Stdin
	os.Stdin = file
	view := ui.New()
	os.Stdin = previousInput
	output, err := os.CreateTemp(t.TempDir(), "menu-output")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	service := manager{
		exists: func(command string) bool { return command == "systemctl" },
		query: func(command string, args ...string) (string, error) {
			if command == "systemctl" {
				return "LoadState=loaded\nUnitFileState=enabled\nActiveState=active\n" +
					"NextElapseUSecRealtime=Tue 2026-11-24 18:36:09 PST\nTimersCalendar=*-*-* 18:30:00\nPersistent=yes\n", nil
			}
			if args[0] == "dump" {
				return "APT::Periodic::Unattended-Upgrade \"0\";\n", nil
			}
			return "PREFERRED='7'\nOPT1='0'\nUPGRADE='0'\n", nil
		},
	}
	previousOutput := os.Stdout
	os.Stdout = output
	err = service.menu(view)
	os.Stdout = previousOutput
	if !errors.Is(err, ui.ErrReturnToMenu) {
		t.Fatalf("menu did not return: %v", err)
	}
	data, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"系统工具  ›  软件包自动更新",
		"软件包：[未安装]", "自动安装更新：关闭", "保存的更新频率：每 7 天",
		"索引刷新：已启用 · 已启动", "下次触发：2026-11-24 18:36:09 PST",
		"周期与调度", "关闭 · 已保存每 7 天", "系统单元：apt-daily.timer", "补跑错过的任务：开启",
		"APT::Periodic::Unattended-Upgrade \"0\";", "\n\n  0/q 返回系统工具",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("menu missing %q", want)
		}
	}
	if strings.Contains(text, "enabled / active") {
		t.Fatal("menu exposed raw timer status")
	}
}

func TestApplyAutomaticUpdates(t *testing.T) {
	for _, scenario := range []struct {
		name, previous              string
		want                        map[string]string
		enable, install, overridden bool
		commands                    []string
	}{
		{name: "enable", enable: true, want: map[string]string{upgradeKey: "1", refreshKey: "1"}, commands: []string{"systemctl enable --now apt-daily-upgrade.timer"}},
		{name: "install then enable", enable: true, install: true, want: map[string]string{upgradeKey: "1", refreshKey: "1"}, commands: []string{
			"apt-get update", "apt-get install -y unattended-upgrades", "systemctl enable --now apt-daily-upgrade.timer",
		}},
		{name: "disable without changing timers", previous: previousPolicy, want: map[string]string{upgradeKey: "0", intervalKey: "7", refreshKey: "2", shutdownKey: "false", shutdownPreference: "true"}},
		{name: "restore frequency and shutdown preference", enable: true, previous: previousPolicy +
			`APT::Periodic::Unattended-Upgrade "0";
Unattended-Upgrade::InstallOnShutdown "false";
ServerTool::Unattended-Upgrade::Interval "7";
ServerTool::Unattended-Upgrade::InstallOnShutdown "true";
`, want: map[string]string{upgradeKey: "7", refreshKey: "2", shutdownKey: "true"}, commands: []string{"systemctl enable --now apt-daily-upgrade.timer"}},
		{name: "later configuration wins", enable: true, overridden: true, previous: previousPolicy, want: map[string]string{upgradeKey: "7"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "99-z-servertool-unattended-upgrades.conf")
			if scenario.previous != "" {
				if err := os.WriteFile(path, []byte(scenario.previous), 0644); err != nil {
					t.Fatal(err)
				}
			}
			installed, timerEnabled := !scenario.install, false
			var commands []string
			service := manager{
				path:   path,
				exists: func(command string) bool { return command != "unattended-upgrade" || installed },
				query: func(command string, args ...string) (string, error) {
					switch command {
					case "apt-config":
						data, err := os.ReadFile(path)
						if err != nil && !os.IsNotExist(err) {
							return "", err
						}
						values := aptValues(string(data))
						if scenario.overridden {
							values[upgradeKey] = "0"
							values[intervalKey] = ""
						}
						var result strings.Builder
						for index := 1; index < len(args); index += 2 {
							if value, found := values[strings.TrimSuffix(args[index+1], "/b")]; found {
								fmt.Fprintf(&result, "%s='%s'\n", args[index], value)
							}
						}
						return result.String(), nil
					case "systemctl":
						if args[len(args)-1] == refreshTimer || timerEnabled {
							return "LoadState=loaded\nActiveState=active\nUnitFileState=enabled\n", nil
						}
						return "LoadState=loaded\nActiveState=inactive\nUnitFileState=disabled\n", nil
					default:
						t.Fatalf("unexpected query %s %v", command, args)
						return "", nil
					}
				},
				run: func(command string, args ...string) error {
					commands = append(commands, command+" "+strings.Join(args, " "))
					if command == "apt-get" && args[0] == "install" {
						installed = true
					}
					if command == "systemctl" {
						timerEnabled = true
					}
					return nil
				},
			}
			err := service.apply(scenario.enable, []byte(scenario.previous), scenario.previous != "")
			if (err != nil) != scenario.overridden {
				t.Fatalf("apply: %v", err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for key, want := range scenario.want {
				if got := aptValues(string(data))[key]; got != want {
					t.Fatalf("%s = %q, want %q (configuration %s)", key, got, want, data)
				}
			}
			if scenario.previous != "" && !strings.Contains(string(data), "Unattended-Upgrade::Origins-Pattern") {
				t.Fatal("native policy was lost")
			}
			if scenario.overridden && string(data) != scenario.previous {
				t.Fatal("configuration was not restored")
			}
			if !reflect.DeepEqual(commands, scenario.commands) {
				t.Fatalf("commands = %v, want %v", commands, scenario.commands)
			}
		})
	}
}

func aptValues(content string) map[string]string {
	values := map[string]string{}
	for _, line := range strings.Split(content, "\n") {
		key, rest, ok := strings.Cut(line, " ")
		if !ok || !strings.HasPrefix(rest, "\"") {
			continue
		}
		literal, err := strconv.QuotedPrefix(rest)
		if err == nil {
			values[key], _ = strconv.Unquote(literal)
		}
	}
	return values
}

func TestScheduleRollsBackAndReloadsAfterRestartFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "90-servertool-schedule.conf")
	previous := timerHeader + "[Timer]\nOnCalendar=daily\n"
	updated := timerHeader + "[Timer]\nOnCalendar=\nOnCalendar=*-*-* 03:00:00\n"
	if err := os.WriteFile(path, []byte(previous), 0644); err != nil {
		t.Fatal(err)
	}
	var commands []string
	restartError := errors.New("timer restart failed")
	service := manager{run: func(command string, args ...string) error {
		commands = append(commands, command+" "+strings.Join(args, " "))
		if len(commands) == 2 {
			return restartError
		}
		return nil
	}}
	if err := service.publishSchedule(upgradeTimer, path, []byte(previous), true, updated); !errors.Is(err, restartError) {
		t.Fatalf("publish: %v", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != previous {
		t.Fatalf("original schedule was not restored: %q, %v", data, err)
	}
	want := []string{"systemctl daemon-reload", "systemctl try-restart apt-daily-upgrade.timer",
		"systemctl daemon-reload", "systemctl try-restart apt-daily-upgrade.timer"}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %v, want %v", commands, want)
	}
}
