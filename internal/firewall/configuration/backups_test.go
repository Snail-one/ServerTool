package configuration_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"snail_tool/internal/firewall/internal/testenv"
	"snail_tool/internal/storage"
)

func TestFirewallBackupsNeverOverwriteAnEarlierSnapshot(t *testing.T) {
	env := testenv.New(t)
	version := env.RuleConfig(t, nil)
	first, err := env.Store().Backup()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env.ConfigPath, []byte("# second version\n"), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := env.Store().Backup()
	if err != nil {
		t.Fatal(err)
	}
	if first == second || storage.ReadFileString(first) != string(version.Content) || storage.ReadFileString(second) != "# second version\n" {
		t.Fatal("consecutive edits must preserve distinct complete backups")
	}
}

func TestFirewallBackupUsesPrivateDataDirectory(t *testing.T) {
	env := testenv.New(t)
	const original = "# readable system configuration\n"
	if err := os.WriteFile(env.ConfigPath, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	// Older backups beside the configuration remain untouched.
	legacy := env.ConfigPath + ".bak.20261021_120000-old"
	if err := os.WriteFile(legacy, []byte("# historical backup\n"), 0644); err != nil {
		t.Fatal(err)
	}
	backup, err := env.Store().Backup()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(backup) != env.BackupDir() || storage.ReadFileString(backup) != original {
		t.Fatalf("backup is not in the tool data directory: %s", backup)
	}
	info, err := os.Stat(backup)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("backup permissions should be 600: %v %v", info, err)
	}
	for _, path := range []string{env.DataDir, filepath.Join(env.DataDir, "backups"), env.BackupDir()} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0700 {
			t.Fatalf("backup directory should be private: %s %v %v", path, info, err)
		}
	}
	oldBackups, err := filepath.Glob(env.ConfigPath + ".bak.*")
	if err != nil || !reflect.DeepEqual(oldBackups, []string{legacy}) || storage.ReadFileString(legacy) != "# historical backup\n" {
		t.Fatalf("old backups changed or new backup appeared beside configuration: %v %v", oldBackups, err)
	}
	info, err = os.Stat(env.ConfigPath)
	if err != nil || info.Mode().Perm() != 0644 || storage.ReadFileString(env.ConfigPath) != original {
		t.Fatalf("system configuration changed during backup: %v %v", info, err)
	}
}

func TestWriteConfigStopsWhenBackupDirectoryIsInvalid(t *testing.T) {
	env := testenv.New(t)
	version := env.RuleConfig(t, nil)
	if err := os.WriteFile(env.DataDir, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	env.CommandOutput = func(name string, args ...string) (string, error) {
		if name != "nft" || len(args) != 3 || args[0] != "-c" || args[1] != "-f" {
			t.Fatalf("must only validate, never load: %s %v", name, args)
		}
		return "", nil
	}
	if err := env.Store().Write("# new configuration\n", version); err == nil {
		t.Fatal("backup directory failure should stop writing")
	}
	if storage.ReadFileString(env.ConfigPath) != string(version.Content) {
		t.Fatal("original configuration changed despite failed backup")
	}
}
