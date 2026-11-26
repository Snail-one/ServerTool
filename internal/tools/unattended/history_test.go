package unattended

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/ui"
)

func TestLatestUpdateRun(t *testing.T) {
	const started = "INFO Starting unattended upgrades script"
	success := []string{started, "INFO Packages that will be upgraded: openssl linux-image-amd64", "INFO All upgrades installed"}
	for _, test := range []struct {
		name  string
		lines []string
		want  updateRun
	}{
		{"successful upgrade", success, updateRun{start: "2026-11-26 01:00:00", result: "升级已安装", packages: []string{"openssl", "linux-image-amd64"}, confirmedPackages: true}},
		{"no updates after earlier success", append(append([]string{}, success...), started, "INFO No packages found that can be upgraded unattended and no pending auto-removals"),
			updateRun{start: "2026-11-26 01:00:03", result: "未发现可升级软件包"}},
		{"simulation and download do not replace confirmed result", append(append([]string{}, success...), started, "INFO Option --dry-run given, *not* performing real actions",
			"INFO Packages that will be upgraded: curl", "INFO All upgrades installed", started, "INFO Allowed origins are: origin=Debian"),
			updateRun{start: "2026-11-26 01:00:00", result: "升级已安装", packages: []string{"openssl", "linux-image-amd64"}, confirmedPackages: true}},
		{"failed upgrade", []string{started, "INFO Packages that will be upgraded: openssl", "ERROR Installing the upgrades failed!",
			"ERROR error message: dpkg returned error code (1)", "ERROR dpkg returned a error! See details"},
			updateRun{start: "2026-11-26 01:00:00", result: "执行报错", packages: []string{"openssl"}, reason: "error message: dpkg returned error code (1)"}},
		{"partial upgrade with explicit package list", []string{started, "INFO Packages that will be upgraded: openssl curl",
			"ERROR Upgrade interrupted", "INFO Packages that were upgraded: openssl"},
			updateRun{start: "2026-11-26 01:00:00", result: "执行报错", packages: []string{"openssl"}, reason: "Upgrade interrupted", confirmedPackages: true}},
		{"no completion found", []string{started, "INFO Packages that will be upgraded: openssl"},
			updateRun{start: "2026-11-26 01:00:00", packages: []string{"openssl"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var log strings.Builder
			for i, line := range test.lines {
				fmt.Fprintf(&log, "2026-11-26 01:00:%02d,322 %s\n", i, line)
			}
			if got := latestUpdateRun(log.String()); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("run = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestUpdateHistoryUsesCustomLogAndRotation(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "updates.log")
	if err := os.WriteFile(file+".1", []byte("2026-11-26 02:00:00,000 INFO Starting unattended upgrades script\n"+
		"2026-11-26 02:00:01,000 INFO Packages that will be upgraded: openssl\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("2026-11-26 02:00:03,000 INFO All upgrades installed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	service := manager{
		exists: func(command string) bool { return command == "systemctl" },
		query: func(command string, args ...string) (string, error) {
			if command == "systemctl" && args[len(args)-1] == upgradeUnit {
				return "ExecMainStartTimestamp=Thu 2026-11-26 03:00:00 UTC\nActiveState=failed\nResult=exit-code\nExecMainStatus=1\n", nil
			}
			if command == "apt-config" {
				return fmt.Sprintf("LOG_DIR='%s/'\nLOG_FILE='updates.log'\n", dir), nil
			}
			t.Fatalf("unexpected query %s %v", command, args)
			return "", nil
		},
	}
	fields := service.updateHistory().fields(false)
	want := []ui.CardField{
		{Label: "最近一次定时服务", Value: "2026-11-26 03:00:00 UTC · 执行失败", Detail: "systemd 结果：exit-code · 退出状态：1"},
		{Label: "最近更新记录", Value: "2026-11-26 02:00:00 · 升级已安装"},
		{Label: "已升级软件包", Value: "openssl"},
	}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("history = %#v, want %#v", fields, want)
	}
}

func TestUpdateHistoryOmitsPlannedPackagesOnFailure(t *testing.T) {
	history := updateHistory{run: updateRun{start: "2026-11-26 02:00:00", result: "执行报错", reason: "dpkg error", packages: []string{"openssl"}}}
	for _, field := range history.fields(true) {
		if strings.Contains(field.Value, "openssl") {
			t.Fatalf("failed run showed planned package: %+v", field)
		}
	}
}

func TestMenuRetriesPartiallyStartedTimers(t *testing.T) {
	t.Setenv("TERM", "dumb")
	t.Setenv("NO_COLOR", "1")
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.conf")
	input, err := os.Create(filepath.Join(dir, "input"))
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if _, err := input.WriteString("a\ny\n\nt\ny\n\nq\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := input.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	previousInput := os.Stdin
	os.Stdin = input
	view := ui.New()
	os.Stdin = previousInput

	var refreshReady, upgradeReady bool
	var savedPolicy string
	var starts []string
	service := manager{
		path:   path,
		root:   func() bool { return true },
		exists: func(command string) bool { return command == "systemctl" || command == "unattended-upgrade" },
		query: func(command string, args ...string) (string, error) {
			if command == "systemctl" {
				switch args[len(args)-1] {
				case upgradeUnit:
					return "", nil
				case refreshTimer, upgradeTimer:
					ready := upgradeReady
					if args[len(args)-1] == refreshTimer {
						ready = refreshReady
					}
					if ready {
						return "LoadState=loaded\nUnitFileState=enabled\nActiveState=active\nNextElapseUSecRealtime=tomorrow\n", nil
					}
					return "LoadState=loaded\nUnitFileState=disabled\nActiveState=inactive\n", nil
				}
			}
			data, err := os.ReadFile(path)
			if err != nil && !os.IsNotExist(err) {
				return "", err
			}
			values := aptValues(string(data))
			values["Unattended-Upgrade::LogDir/d"] = dir
			var result strings.Builder
			for i := 1; i+1 < len(args); i += 2 {
				if value, found := values[args[i+1]]; found {
					fmt.Fprintf(&result, "%s='%s'\n", args[i], value)
				}
			}
			return result.String(), nil
		},
		run: func(command string, args ...string) error {
			starts = append(starts, command+" "+strings.Join(args, " "))
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if len(starts) == 1 {
				refreshReady = true
				savedPolicy = string(data)
				return errors.New("upgrade timer failed to start")
			}
			if string(data) != savedPolicy {
				t.Error("timer retry changed the saved policy")
			}
			upgradeReady = true
			return nil
		},
	}

	output, err := os.Create(filepath.Join(dir, "output"))
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	previousOutput := os.Stdout
	os.Stdout = output
	err = service.menu(view)
	os.Stdout = previousOutput
	if !errors.Is(err, ui.ErrReturnToMenu) {
		t.Fatalf("menu did not return: %v", err)
	}
	want := []string{"systemctl enable --now apt-daily.timer apt-daily-upgrade.timer", "systemctl enable --now apt-daily-upgrade.timer"}
	if !reflect.DeepEqual(starts, want) || !refreshReady || !upgradeReady {
		t.Fatalf("calls = %v, want %v", starts, want)
	}
	text, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(text), "  t   重试启动定时器") != 1 || !strings.Contains(string(text), "APT 定时器已启用并启动") {
		t.Fatalf("unexpected retry menu output:\n%s", text)
	}
}
