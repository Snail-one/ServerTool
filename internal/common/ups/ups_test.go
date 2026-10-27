package ups

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/shared"
)

func setupBackupStore(t *testing.T) {
	t.Helper()
	previous := backupStore
	backupStore = shared.BackupStore{Root: filepath.Join(t.TempDir(), "snailtool"), Module: "ups"}
	t.Cleanup(func() { backupStore = previous })
}

func TestNUTSystemdServicesOrder(t *testing.T) {
	got := nutSystemdServices()
	want := []nutSystemdService{
		{
			unit:      "nut-driver@ups",
			unitFiles: []string{"nut-driver@ups.service", "nut-driver@.service"},
			optional:  true,
		},
		{unit: "nut-server"},
		{unit: "nut-monitor"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("nutSystemdServices() = %#v, want %#v", got, want)
	}
}

func TestUPSDescriptionPreservesTextAndEscapesConfigurationCharacters(t *testing.T) {
	for _, item := range []struct{ raw, want string }{
		{`  "中文 USB UPS"  `, "中文 USB UPS"},
		{`John's UPS`, `John's UPS`},
		{`UPS "A" \ USB`, `UPS \"A\" \\ USB`},
	} {
		if got, err := normalizeUPSDescription(item.raw); err != nil || got != item.want {
			t.Fatalf("description %q: got %q, err %v", item.raw, got, err)
		}
	}
	for _, raw := range []string{"", `""`, `"  "`, "UPS\xff", "UPS\ufffd", "UPS\n[other]", "UPS\u200b"} {
		if _, err := normalizeUPSDescription(raw); err == nil {
			t.Fatalf("invalid description accepted: %q", raw)
		}
	}
}

func TestApplyNUTStandaloneModeReplacesExistingMode(t *testing.T) {
	input := "# sample\nMODE=netserver\n"
	got := applyNUTStandaloneMode(input)
	want := "# sample\nMODE=standalone\n"
	if got != want {
		t.Fatalf("applyNUTStandaloneMode() = %q, want %q", got, want)
	}
}

func TestApplyNUTStandaloneModeAppendsMissingMode(t *testing.T) {
	input := "# sample\n"
	got := applyNUTStandaloneMode(input)
	if !strings.Contains(got, "MODE=standalone\n") {
		t.Fatalf("MODE=standalone missing:\n%s", got)
	}
}

func TestReplaceNUTSectionIsIdempotent(t *testing.T) {
	block := buildUPSDeviceBlock(upsDeviceConfig{
		VendorID:  "0463",
		ProductID: "ffff",
		Desc:      "SANTAK TG-BOX 850 USB UPS",
	})
	input := `[old]
value = keep

[ups]
driver = old
port = old

[next]
value = keep
`

	got := replaceNUTSection(input, "ups", block)
	got = replaceNUTSection(got, "ups", block)

	if strings.Count(got, "[ups]") != 1 {
		t.Fatalf("expected one [ups] section:\n%s", got)
	}
	if strings.Contains(got, "driver = old") {
		t.Fatalf("old ups section remained:\n%s", got)
	}
	for _, kept := range []string{"[old]", "[next]", "driver = usbhid-ups", `desc = "SANTAK TG-BOX 850 USB UPS"`} {
		if !strings.Contains(got, kept) {
			t.Fatalf("missing %q in:\n%s", kept, got)
		}
	}
}

func TestApplyUPSMonConfigReplacesGeneratedLines(t *testing.T) {
	input := `# keep custom comment
MONITOR ups@localhost 1 monuser oldpass master
SHUTDOWNCMD "/old/shutdown"
POWERDOWNFLAG /old/flag
NOTIFYFLAG ONLINE SYSLOG
NOTIFYFLAG COMMBAD SYSLOG
`

	got := applyUPSMonConfig(input, "newpass")

	for _, old := range []string{"oldpass", "NOTIFYFLAG ONLINE SYSLOG"} {
		if strings.Contains(got, old) {
			t.Fatalf("old generated line remained:\n%s", got)
		}
	}
	for _, want := range []string{
		"# keep custom comment",
		`SHUTDOWNCMD "/old/shutdown"`,
		"POWERDOWNFLAG /old/flag",
		"NOTIFYFLAG COMMBAD SYSLOG",
		"MONITOR ups@localhost 1 monuser newpass master",
		"NOTIFYCMD /usr/sbin/upssched",
		"NOTIFYFLAG ONBATT EXEC",
		"NOTIFYFLAG ONLINE EXEC",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestWriteNUTConfigFileRequiresExistingNUTConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nut.conf")

	err := writeNUTConfigFile(nutConfigFile{path: path, mode: 0640, owner: rootNutOwner}, applyNUTStandaloneMode)
	if err == nil {
		t.Fatal("expected missing NUT config file to fail")
	}
	if !strings.Contains(err.Error(), "配置文件不存在") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("missing NUT config file should not be created, stat err=%v", statErr)
	}
}

func TestWriteNUTConfigFileCanCreateManagedScript(t *testing.T) {
	setupBackupStore(t)
	path := filepath.Join(t.TempDir(), "ups-onbatt-actions.sh")

	err := writeNUTConfigFile(nutConfigFile{path: path, mode: 0700, owner: fileOwner{}}, applyUPSOnBattScript)
	if err != nil {
		t.Fatal(err)
	}
	if got := readTestFile(t, path); !strings.Contains(got, "upsmon -c fsd") {
		t.Fatalf("script was not written:\n%s", got)
	}
}

func TestApplyUPSSchedConfigReplacesGeneratedLines(t *testing.T) {
	input := `# keep custom comment
CMDSCRIPT /old/script.sh
PIPEFN /tmp/old.pipe
LOCKFN /tmp/old.lock
AT ONBATT * START-TIMER old_timer 5
AT ONLINE * CANCEL-TIMER old_timer
AT COMMBAD * EXECUTE custom_handler
`

	got := applyUPSSchedConfig(input)
	got = applyUPSSchedConfig(got)

	for _, old := range []string{"/old/script.sh", "/tmp/old.pipe", "/tmp/old.lock", "old_timer"} {
		if strings.Contains(got, old) {
			t.Fatalf("old upssched line remained:\n%s", got)
		}
	}
	for _, want := range []string{
		"# keep custom comment",
		"AT COMMBAD * EXECUTE custom_handler",
		"CMDSCRIPT /usr/local/sbin/ups-onbatt-actions.sh",
		"PIPEFN /run/nut/upssched.pipe",
		"LOCKFN /run/nut/upssched.lock",
		"AT ONBATT * START-TIMER onbatt_shutdown 60",
		"AT ONLINE * CANCEL-TIMER onbatt_shutdown",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	for _, once := range []string{"CMDSCRIPT", "PIPEFN", "LOCKFN", "AT ONBATT", "AT ONLINE"} {
		if strings.Count(got, once) != 1 {
			t.Fatalf("expected one %q line in:\n%s", once, got)
		}
	}
	if !hasUPSSchedConfig(got) {
		t.Fatalf("generated upssched config did not pass status check:\n%s", got)
	}
}

func TestApplyUPSOnBattScript(t *testing.T) {
	got := applyUPSOnBattScript("old content")

	for _, want := range []string{
		"#!/bin/sh",
		`case "$1" in`,
		"onbatt_shutdown)",
		`logger -t nut "ONBATT timer expired (60s), triggering FSD"`,
		"exec /usr/sbin/upsmon -c fsd",
		`logger -t nut "upssched called with unknown event: $1"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "old content") {
		t.Fatalf("old script content remained:\n%s", got)
	}
	if !hasUPSOnBattScript(got) {
		t.Fatalf("generated script did not pass status check:\n%s", got)
	}
}

func TestUPSManagedFileModesAndOwners(t *testing.T) {
	for _, file := range nutConfigFiles {
		switch file.path {
		case upsOnBattScript:
			if file.mode != 0700 {
				t.Fatalf("script mode = %v, want 0700", file.mode)
			}
			if file.owner != nutOwner {
				t.Fatalf("script owner = %#v, want %#v", file.owner, nutOwner)
			}
		default:
			if !strings.HasPrefix(file.path, nutConfigDir+"/") {
				t.Fatalf("unexpected managed file path: %s", file.path)
			}
			if file.mode != 0640 {
				t.Fatalf("%s mode = %v, want 0640", file.path, file.mode)
			}
			if file.owner != rootNutOwner {
				t.Fatalf("%s owner = %#v, want %#v", file.path, file.owner, rootNutOwner)
			}
			if shouldEnforceFileMetadata(file) {
				t.Fatalf("%s should only be checked, not chmod/chowned", file.path)
			}
		}
	}
	if !shouldEnforceFileMetadata(upsOnBattScriptFile) {
		t.Fatal("expected script metadata to be enforced")
	}
}

func TestUPSStatusHelpers(t *testing.T) {
	upsmon := `MONITOR ups@localhost 1 monuser password master
NOTIFYCMD /usr/sbin/upssched
NOTIFYFLAG ONBATT EXEC
NOTIFYFLAG ONLINE EXEC
`
	if !hasUPSMonConfig(upsmon) {
		t.Fatalf("expected upsmon config to pass")
	}
	if hasUPSMonConfig(strings.Replace(upsmon, "NOTIFYFLAG ONLINE EXEC\n", "", 1)) {
		t.Fatalf("expected missing ONLINE flag to fail")
	}

	upssched := applyUPSSchedConfig("")
	if !hasUPSSchedConfig(upssched) {
		t.Fatalf("expected upssched config to pass")
	}
	if hasUPSSchedConfig(strings.Replace(upssched, "LOCKFN /run/nut/upssched.lock\n", "", 1)) {
		t.Fatalf("expected missing LOCKFN to fail")
	}
}

func TestFilePermissionMatches(t *testing.T) {
	path := filepath.Join(t.TempDir(), "script.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0644); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	owner, group := fileOwnerNames(info)
	file := nutConfigFile{path: path, mode: 0700, owner: fileOwner{user: owner, group: group}}

	if filePermissionMatches(file) {
		t.Fatal("expected wrong mode to fail")
	}
	if err := os.Chmod(path, 0700); err != nil {
		t.Fatal(err)
	}
	if !filePermissionMatches(file) {
		t.Fatal("expected matching owner and 0700 mode to pass")
	}
}

func TestBackupNUTConfigFileKeepsOriginalBackup(t *testing.T) {
	setupBackupStore(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "nut.conf")

	if err := os.WriteFile(path, []byte("official\n"), 0640); err != nil {
		t.Fatal(err)
	}

	backup, created, err := backupNUTConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected first backup to be created")
	}
	if backup != originalNUTBackupPath(path) {
		t.Fatalf("backup path = %q, want %q", backup, originalNUTBackupPath(path))
	}
	if got, err := backupStore.ReadOriginal(path); err != nil || string(got.Data) != "official\n" {
		t.Fatalf("backup content = %q, err=%v, want official", got.Data, err)
	}

	if err := os.WriteFile(path, []byte("modified\n"), 0640); err != nil {
		t.Fatal(err)
	}
	backup, created, err = backupNUTConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("expected existing original backup to be preserved")
	}
	if got, err := backupStore.ReadOriginal(path); err != nil || string(got.Data) != "official\n" {
		t.Fatalf("backup was overwritten, got %q, err=%v", got.Data, err)
	}
}

func TestExistingNUTBackupsFindsOriginalBackup(t *testing.T) {
	setupBackupStore(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "upsmon.conf")

	if err := os.WriteFile(path, []byte("official\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if _, _, err := backupNUTConfigFile(path); err != nil {
		t.Fatal(err)
	}

	originalFiles := nutConfigFiles
	nutConfigFiles = []nutConfigFile{{path: path, mode: 0644}}
	defer func() {
		nutConfigFiles = originalFiles
	}()

	backups := existingNUTBackups()
	if len(backups) != 1 || backups[0].path != path {
		t.Fatalf("unexpected backups: %#v", backups)
	}
}

func TestRestoreNUTBackupFileRestoresOriginalBackup(t *testing.T) {
	setupBackupStore(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "ups.conf")

	if err := os.WriteFile(path, []byte("restored\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if _, _, err := backupNUTConfigFile(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("broken\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}

	err := restoreNUTBackupFile(nutConfigFile{path: path, mode: 0644})
	if err != nil {
		t.Fatal(err)
	}

	if got := readTestFile(t, path); got != "restored\n" {
		t.Fatalf("restored content = %q, want restored", got)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0640 {
		t.Fatalf("original file permissions were not restored: %v %v", info, err)
	}
}

func TestDeleteNUTBackupFileDeletesBackupOnly(t *testing.T) {
	setupBackupStore(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "ups.conf")
	backup := originalNUTBackupPath(path)

	if err := os.WriteFile(path, []byte("current\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := backupNUTConfigFile(path); err != nil {
		t.Fatal(err)
	}

	if err := backupStore.RemoveOriginal(path); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("expected backup to be deleted, stat err=%v", err)
	}
	if got := readTestFile(t, path); got != "current\n" {
		t.Fatalf("current file changed, got %q", got)
	}
}

func TestUPSBackupsDoNotUseLegacyLocations(t *testing.T) {
	setupBackupStore(t)
	path := filepath.Join(t.TempDir(), "ups.conf")
	if err := os.WriteFile(path, []byte("current\n"), 0640); err != nil {
		t.Fatal(err)
	}
	legacy := path + ".bak"
	if err := os.WriteFile(legacy, []byte("legacy\n"), 0640); err != nil {
		t.Fatal(err)
	}
	previous := nutConfigFiles
	nutConfigFiles = []nutConfigFile{{path: path, mode: 0640}}
	t.Cleanup(func() { nutConfigFiles = previous })
	if got := existingNUTBackups(); len(got) != 0 {
		t.Fatalf("legacy backup was discovered: %v", got)
	}
	if err := restoreNUTBackupFile(nutConfigFiles[0]); !os.IsNotExist(err) {
		t.Fatalf("restore must only use the new directory: %v", err)
	}
	if _, created, err := backupNUTConfigFile(path); err != nil || !created {
		t.Fatalf("legacy backup prevented creation in the new directory: %v %v", created, err)
	}
	if err := restoreNUTBackupFile(nutConfigFiles[0]); err != nil {
		t.Fatal(err)
	}
	if got := readTestFile(t, path); got != "current\n" {
		t.Fatalf("restored legacy contents: %q", got)
	}
	if err := backupStore.RemoveOriginal(nutConfigFiles[0].path); err != nil {
		t.Fatal(err)
	}
	if got := readTestFile(t, legacy); got != "legacy\n" {
		t.Fatalf("legacy file was modified: %q", got)
	}
}
