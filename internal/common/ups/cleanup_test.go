package ups

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/shared"
)

func setupCleanup(t *testing.T, files map[string]string) string {
	t.Helper()
	setupBackupStore(t)
	previousFiles := nutConfigFiles
	previousRun, previousExists, previousUnit := cleanupCommandRun, cleanupCommandExists, cleanupUnitExists
	t.Cleanup(func() {
		nutConfigFiles = previousFiles
		cleanupCommandRun, cleanupCommandExists, cleanupUnitExists = previousRun, previousExists, previousUnit
	})
	dir := t.TempDir()
	nutConfigFiles = nil
	for _, name := range []string{"nut.conf", "ups.conf", "upsd.conf", "upsd.users", "upsmon.conf", "upssched.conf", "ups-onbatt-actions.sh"} {
		content, exists := files[name]
		if !exists {
			continue
		}
		path := filepath.Join(dir, name)
		mode := os.FileMode(0640)
		if strings.HasSuffix(name, ".sh") {
			mode = 0700
		}
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
		nutConfigFiles = append(nutConfigFiles, nutConfigFile{path: path, mode: mode})
	}
	cleanupCommandExists = func(string) bool { return true }
	cleanupUnitExists = func(string) bool { return true }
	cleanupCommandRun = func(name string, args ...string) error {
		t.Fatalf("unexpected service mutation: %s %v", name, args)
		return nil
	}
	return dir
}

func TestCleanupUPSRestoresOriginalContentPermissionsAndOwner(t *testing.T) {
	dir := setupCleanup(t, map[string]string{"ups.conf": "[original]\ndriver=keep\n"})
	path := filepath.Join(dir, "ups.conf")
	if _, _, err := backupNUTConfigFile(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[ups]\ndriver=usbhid-ups\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	var calls []string
	cleanupCommandRun = func(name string, args ...string) error {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if shared.ReadFileString(path) != "[ups]\ndriver=usbhid-ups\n" {
			t.Fatal("UPS files must only change after services are stopped")
		}
		return nil
	}
	if err := Cleanup(nil); err != nil {
		t.Fatal(err)
	}
	if shared.ReadFileString(path) != "[original]\ndriver=keep\n" {
		t.Fatal("original configuration not restored")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0640 {
		t.Fatalf("original permissions not restored: %v %v", info, err)
	}
	if !reflect.DeepEqual(calls, []string{
		"systemctl disable --now nut-monitor.service",
		"systemctl disable --now nut-server.service",
		"systemctl disable --now nut-driver@ups.service",
	}) {
		t.Fatal(calls)
	}
	if _, err := backupStore.ReadOriginal(path); err != nil {
		t.Fatal("configuration cleanup must leave backups for the final cleanup step")
	}
}

func TestCleanupUPSWithoutBackupRemovesKnownSettingsAndKeepsOtherDevices(t *testing.T) {
	dir := setupCleanup(t, map[string]string{
		"nut.conf":              "# keep\nMODE=standalone\n",
		"ups.conf":              "[other]\ndriver=keep\n\n[ups]\ndriver=usbhid-ups\n",
		"upsd.conf":             "LISTEN 192.0.2.1 3493\n" + upsListenLine + "\n",
		"upsd.users":            "[admin]\nactions=SET\n\n[monuser]\npassword=secret\nupsmon master\n",
		"upsmon.conf":           "MONITOR other@localhost 1 admin secret master\n" + applyUPSMonConfig("", "secret"),
		"upssched.conf":         "AT COMMBAD * EXECUTE custom\n" + applyUPSSchedConfig(""),
		"ups-onbatt-actions.sh": applyUPSOnBattScript(""),
	})
	cleanupCommandRun = func(string, ...string) error { return nil }
	if err := Cleanup(nil); err != nil {
		t.Fatal(err)
	}
	for name, expected := range map[string]string{
		"nut.conf": "MODE=none", "ups.conf": "[other]", "upsd.conf": "LISTEN 192.0.2.1 3493",
		"upsd.users": "[admin]", "upsmon.conf": "MONITOR other@localhost", "upssched.conf": "AT COMMBAD * EXECUTE custom",
	} {
		content := shared.ReadFileString(filepath.Join(dir, name))
		if !strings.Contains(content, expected) || strings.Contains(content, "[ups]") || strings.Contains(content, "[monuser]") || strings.Contains(content, "onbatt_shutdown") || strings.Contains(content, "NOTIFYCMD /usr/sbin/upssched") {
			t.Fatalf("unrelated settings lost or tool settings remain in %s: %s", name, content)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "ups-onbatt-actions.sh")); !os.IsNotExist(err) {
		t.Fatalf("managed UPS script remains: %v", err)
	}
	cleanupCommandRun = func(name string, args ...string) error {
		t.Fatalf("already-clean UPS must not stop services again: %s %v", name, args)
		return nil
	}
	if err := Cleanup(nil); err != nil {
		t.Fatal(err)
	}
}

func TestCleanupUPSPreflightAndServiceFailuresPreserveConfiguration(t *testing.T) {
	for _, scenario := range []string{"corrupt backup", "modified script", "service failure"} {
		t.Run(scenario, func(t *testing.T) {
			files := map[string]string{"nut.conf": "MODE=standalone\n"}
			if scenario == "modified script" {
				files["ups-onbatt-actions.sh"] = applyUPSOnBattScript("") + "# user change\n"
			}
			dir := setupCleanup(t, files)
			path := filepath.Join(dir, "nut.conf")
			if scenario == "corrupt backup" {
				backup, _, err := backupNUTConfigFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(backup, []byte("invalid json"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "service failure" {
				cleanupCommandRun = func(string, ...string) error { return errors.New("systemctl failed") }
			}
			if err := Cleanup(nil); err == nil {
				t.Fatal("expected cleanup failure")
			}
			if shared.ReadFileString(path) != "MODE=standalone\n" {
				t.Fatal("UPS files changed before all prerequisites succeeded")
			}
		})
	}
}
