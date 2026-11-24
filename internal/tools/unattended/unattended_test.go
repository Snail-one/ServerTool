package unattended

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestApplyAutomaticUpdates(t *testing.T) {
	for _, scenario := range []struct {
		name, previous, want        string
		enable, install, overridden bool
		commands                    []string
	}{
		{name: "enable", enable: true, want: enabledConfig, commands: []string{"systemctl enable --now apt-daily-upgrade.timer"}},
		{name: "install then enable", enable: true, install: true, want: enabledConfig, commands: []string{
			"apt-get update", "apt-get install -y unattended-upgrades", "systemctl enable --now apt-daily-upgrade.timer",
		}},
		{name: "disable without changing timers", previous: enabledConfig, want: disabledConfig},
		{name: "later configuration wins", enable: true, overridden: true, previous: disabledConfig, want: disabledConfig},
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
						if string(data) == enabledConfig && !scenario.overridden {
							return "REFRESH='1'\nUPGRADE='1'\nREBOOT='false'\n", nil
						}
						return "UPGRADE='0'\n", nil
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
			if data, err := os.ReadFile(path); err != nil || string(data) != scenario.want {
				t.Fatalf("managed configuration = %q, %v", data, err)
			}
			if !reflect.DeepEqual(commands, scenario.commands) {
				t.Fatalf("commands = %v, want %v", commands, scenario.commands)
			}
		})
	}
}
