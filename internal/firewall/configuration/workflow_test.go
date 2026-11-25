package configuration_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/firewall/configuration/generate"
	"snail_tool/internal/firewall/internal/testenv"
	"snail_tool/internal/storage"
	"snail_tool/internal/ui"
)

func TestWriteConfigValidatesBeforeBackupAndWrite(t *testing.T) {
	for _, valid := range []bool{false, true} {
		t.Run(map[bool]string{false: "invalid", true: "valid"}[valid], func(t *testing.T) {
			env := testenv.New(t)
			const original = "# original rules\n"
			if err := os.WriteFile(env.ConfigPath, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			content, _ := generate.Build([]int{2222}, true, true)
			env.CommandOutput = func(name string, args ...string) (string, error) {
				if name != "nft" || len(args) != 3 || args[0] != "-c" || args[1] != "-f" {
					t.Fatalf("must validate without applying: %s %v", name, args)
				}
				if got := storage.ReadFileString(args[2]); got != content {
					t.Fatal("validated content differs from proposed config")
				}
				if got := storage.ReadFileString(env.ConfigPath); got != original {
					t.Fatal("file changed before validation")
				}
				if !valid {
					return "syntax error", errors.New("invalid configuration")
				}
				return "", nil
			}
			err := env.Store().Write(content)
			backups, _ := filepath.Glob(filepath.Join(env.BackupDir(), "nftables.conf.bak.*"))
			if !valid {
				if err == nil || storage.ReadFileString(env.ConfigPath) != original || len(backups) != 0 {
					t.Fatalf("failed validation modified config or made backup: err=%v backups=%v", err, backups)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if storage.ReadFileString(env.ConfigPath) != content || len(backups) != 1 || storage.ReadFileString(backups[0]) != original {
				t.Fatal("write did not preserve original in backup")
			}
			if info, err := os.Stat(env.ConfigPath); err != nil || info.Mode().Perm() != 0600 {
				t.Fatalf("existing file permissions not preserved: info=%v err=%v", info, err)
			}
		})
	}
}

func TestEditConfigUsesFirstAvailableEditorAndBacksUp(t *testing.T) {
	for _, available := range [][]string{{"vim", "vi", "nano"}, {"vi", "nano"}, {"nano"}, nil} {
		t.Run(strings.Join(available, "+"), func(t *testing.T) {
			env := testenv.New(t)
			const original = "# existing firewall\n"
			if err := os.WriteFile(env.ConfigPath, []byte(original), 0644); err != nil {
				t.Fatal(err)
			}
			env.CommandExists = func(name string) bool {
				for _, candidate := range available {
					if name == candidate {
						return true
					}
				}
				return false
			}
			launched := ""
			validations, loads := 0, 0
			env.CommandRun = func(name string, args ...string) error {
				launched = name
				draft := env.EditPath(t, args)
				if storage.ReadFileString(draft) != original || storage.ReadFileString(env.ConfigPath) != original {
					t.Fatal("editing copy must start with the original configuration")
				}
				return os.WriteFile(draft, []byte("# edited\n"), 0600)
			}
			env.CommandOutput = func(name string, args ...string) (string, error) {
				if launched == "" || storage.ReadFileString(env.ConfigPath) != "# edited\n" {
					t.Fatal("validation must run after the editor saves and exits")
				}
				switch {
				case name == "nft" && reflect.DeepEqual(args, []string{"-c", "-f", env.ConfigPath}):
					validations++
				case name == "nft" && reflect.DeepEqual(args, []string{"-f", env.ConfigPath}):
					if validations != 1 {
						t.Fatal("load must follow validation")
					}
					loads++
				default:
					t.Fatalf("unexpected command: %s %v", name, args)
				}
				return "", nil
			}
			err := env.EditConfig()
			backups, _ := filepath.Glob(filepath.Join(env.BackupDir(), "nftables.conf.bak.*"))
			if len(available) == 0 {
				if err == nil || !strings.Contains(err.Error(), "未找到 vim、vi、nano") || launched != "" || validations != 0 || len(backups) != 0 || storage.ReadFileString(env.ConfigPath) != original {
					t.Fatalf("missing editors must report error without modifications: err=%v editor=%s", err, launched)
				}
				return
			}
			if err != nil || launched != available[0] || validations != 1 || loads != 1 || len(backups) != 1 || storage.ReadFileString(backups[0]) != original {
				t.Fatalf("editor or backup incorrect: err=%v editor=%s backups=%v", err, launched, backups)
			}
		})
	}
}

func TestEditConfigValidationFailurePreservesEditsAndReportsDetails(t *testing.T) {
	env := testenv.New(t)
	env.CommandExists = func(name string) bool { return name == "vim" }
	env.CommandRun = func(name string, args ...string) error {
		if name != "vim" {
			t.Fatalf("unexpected editor: %s", name)
		}
		return os.WriteFile(env.EditPath(t, args), []byte("invalid nft config\n"), 0600)
	}
	env.CommandOutput = func(name string, args ...string) (string, error) {
		if name != "nft" || !reflect.DeepEqual(args, []string{"-c", "-f", env.ConfigPath}) {
			t.Fatalf("must only check configuration: %s %v", name, args)
		}
		return "nftables.conf:1: syntax error", errors.New("exit status 1")
	}
	err := env.EditConfig()
	if err == nil || !strings.Contains(err.Error(), "配置校验失败") || !strings.Contains(err.Error(), "syntax error") {
		t.Fatalf("missing validation error details: %v", err)
	}
	if storage.ReadFileString(env.ConfigPath) != "invalid nft config\n" {
		t.Fatal("failed validation must retain edits for correction")
	}
}

func TestLoadConfigChecksAndConfirmsBeforeApplying(t *testing.T) {
	for _, scenario := range []string{"missing", "empty", "invalid", "cancel", "changed", "success", "load failure", "nonroot"} {
		t.Run(scenario, func(t *testing.T) {
			env := testenv.New(t)
			const content = "table inet snail {}\n"
			if scenario != "missing" {
				initial := content
				if scenario == "empty" {
					initial = " \n"
				}
				if err := os.WriteFile(env.ConfigPath, []byte(initial), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "nonroot" {
				env.IsRoot = func() bool { return false }
			}
			var commands []string
			env.CommandOutput = func(name string, args ...string) (string, error) {
				if name != "nft" {
					t.Fatalf("unexpected command: %s %v", name, args)
				}
				switch {
				case reflect.DeepEqual(args, []string{"-c", "-f", env.ConfigPath}):
					commands = append(commands, "check")
					if scenario == "invalid" {
						return "syntax error", errors.New("exit status 1")
					}
					if scenario == "changed" {
						if err := os.WriteFile(env.ConfigPath, []byte("# changed\n"), 0644); err != nil {
							t.Fatal(err)
						}
					}
					return "", nil
				case reflect.DeepEqual(args, []string{"-f", env.ConfigPath}):
					commands = append(commands, "load")
					if scenario == "load failure" {
						return "netlink: permission denied", errors.New("exit status 1")
					}
					return "", nil
				default:
					t.Fatalf("unexpected nft arguments: %v", args)
					return "", nil
				}
			}
			input := "y\n"
			if scenario == "cancel" {
				input = "n\n"
			}
			err := env.LoadConfig(testenv.Input(t, input))
			wantErr := scenario != "success" && scenario != "cancel"
			if scenario == "cancel" && !errors.Is(err, ui.ErrReturnToMenu) || scenario != "cancel" && (err != nil) != wantErr {
				t.Fatalf("err=%v, wantErr=%v", err, wantErr)
			}
			var want []string
			switch scenario {
			case "missing", "empty", "nonroot":
			case "success", "load failure":
				want = []string{"check", "load"}
			default:
				want = []string{"check"}
			}
			if !reflect.DeepEqual(commands, want) {
				t.Fatalf("commands=%v, want %v", commands, want)
			}
			if scenario == "invalid" && !strings.Contains(err.Error(), "syntax error") {
				t.Fatalf("validation details missing: %v", err)
			}
			if scenario == "load failure" && !strings.Contains(err.Error(), "netlink: permission denied") {
				t.Fatalf("load error details missing: %v", err)
			}
			if scenario == "success" || scenario == "cancel" || scenario == "invalid" || scenario == "load failure" {
				if storage.ReadFileString(env.ConfigPath) != content {
					t.Fatal("loading must preserve the saved configuration")
				}
			}
		})
	}
}
