package firewall

import (
	"bytes"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/firewall/configuration/generate"
	autostartconfig "snail_tool/internal/firewall/settings/autostart"
	"snail_tool/internal/storage"
)

// Model kernel/service state; unexpected commands fail rather than touching the host.
func originalFixture(t *testing.T, boot string) (*string, *bool, *[][]string) {
	t.Helper()
	setupTest(t)
	preserveOriginal = saveOriginalState
	loaded := false
	var commands [][]string
	commandExists = func(name string) bool { return name == "nft" || name == "systemctl" }
	commandOutput = func(name string, args ...string) (string, error) {
		switch {
		case name == "systemctl" && len(args) > 0 && args[0] == "show":
			return autostartProperties(boot), nil
		case name == "nft" && reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}):
			objects := `{"table":{"family":"ip","name":"docker"}}`
			if loaded {
				objects += `,{"table":{"family":"inet","name":"snail"}}`
			}
			return `{"nftables":[` + objects + `]}`, nil
		case name == "nft" && reflect.DeepEqual(args, []string{"delete", "table", "inet", "snail"}):
			commands = append(commands, append([]string{name}, args...))
			loaded = false
			return "", nil
		case name == "nft" && len(args) == 3 && args[0] == "-c" && args[1] == "-f":
			return "", nil
		case name == "nft" && reflect.DeepEqual(args, []string{"-f", configPath}):
			commands = append(commands, append([]string{name}, args...))
			loaded = true
			return "", nil
		default:
			t.Fatalf("unexpected command: %s %v", name, args)
			return "", errors.New("unexpected command")
		}
	}
	commandRun = func(name string, args ...string) error {
		commands = append(commands, append([]string{name}, args...))
		switch {
		case name == "systemctl" && reflect.DeepEqual(args, []string{"disable", autostartconfig.Unit}):
			boot = "disabled"
		case name == "systemctl" && reflect.DeepEqual(args, []string{"disable", "--runtime", autostartconfig.Unit}):
			boot = "disabled"
		case name == "systemctl" && reflect.DeepEqual(args, []string{"enable", autostartconfig.Unit}):
			boot = "enabled"
		case name == "systemctl" && reflect.DeepEqual(args, []string{"enable", "--runtime", autostartconfig.Unit}):
			boot = "enabled-runtime"
		default:
			t.Fatalf("unexpected mutation: %s %v", name, args)
		}
		return nil
	}
	return &boot, &loaded, &commands
}

func TestCleanupRestoresFirstConfigAndAutostartWithoutFlushingOtherTables(t *testing.T) {
	for _, exists := range []bool{true, false} {
		for _, boot := range []string{"disabled", "enabled", "enabled-runtime"} {
			name := boot + "/absent"
			if exists {
				name = boot + "/existing"
			}
			t.Run(name, func(t *testing.T) {
				state, loaded, commands := originalFixture(t, boot)
				const initial = "# pre-tool configuration\ntable inet custom { }\n"
				if exists {
					if err := os.WriteFile(configPath, []byte(initial), 0600); err != nil {
						t.Fatal(err)
					}
				}
				config, _ := generate.Build([]int{2222}, false, false)
				if err := configStore().Write(config); err != nil {
					t.Fatal(err)
				}
				first, err := readOriginalState()
				if err != nil || first == nil || first.Autostart != boot || (first.Config != nil) != exists {
					t.Fatalf("missing pre-mutation snapshot: %+v %v", first, err)
				}
				if err := configStore().Apply([]byte(config)); err != nil {
					t.Fatal(err)
				}
				// Later changes must never replace the first snapshot.
				*state = "enabled-runtime"
				if boot == "enabled" {
					*state = "disabled"
				} else if boot == "enabled-runtime" {
					*state = "enabled"
				}
				if err := configStore().Write(config + "\n# later change\n"); err != nil {
					t.Fatal(err)
				}
				later, err := readOriginalState()
				if err != nil || !reflect.DeepEqual(first, later) {
					t.Fatalf("first snapshot replaced: %v", err)
				}
				if err := Cleanup(nil); err != nil {
					t.Fatal(err)
				}
				data, present, err := storage.ReadRegularFile(configPath)
				if err != nil || present != exists || exists && string(data) != initial {
					t.Fatalf("original config not restored: present=%v data=%q err=%v", present, data, err)
				}
				if exists {
					info, _ := os.Stat(configPath)
					if info.Mode().Perm() != 0600 {
						t.Fatal("original permissions not restored")
					}
				}
				if *loaded || *state != boot {
					t.Fatalf("runtime/boot state not restored: loaded=%v boot=%s commands=%v", *loaded, *state, *commands)
				}
				if err := Cleanup(nil); err != nil {
					t.Fatalf("cleanup must be repeatable: %v", err)
				}
				if original, err := readOriginalState(); err != nil || original == nil {
					t.Fatal("cleanup removed the recovery snapshot")
				}
			})
		}
	}
}

func TestCleanupCancellationAndConcurrentEditNeverMutate(t *testing.T) {
	for _, answer := range []string{"n\n", "\n", ""} {
		t.Run("answer="+answer, func(t *testing.T) {
			_, _, commands := originalFixture(t, "disabled")
			config, _ := generate.Build([]int{22}, false, false)
			if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
				t.Fatal(err)
			}
			err := CleanupWithConfirm(inputUI(t, answer))
			if err == nil || len(*commands) != 0 || storage.ReadFileString(configPath) != config {
				t.Fatalf("missing/refused confirmation changed state: commands=%v err=%v", *commands, err)
			}
			entries, _ := (storage.BackupStore{Root: dataDir, Module: "firewall"}).List()
			if len(entries) != 0 {
				t.Fatal("cancellation must not create backups or first snapshots")
			}
		})
	}
	t.Run("concurrent edit", func(t *testing.T) {
		_, _, commands := originalFixture(t, "disabled")
		config, _ := generate.Build([]int{22}, false, false)
		if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
			t.Fatal(err)
		}
		plan, err := cleanupService().Prepare()
		if err != nil {
			t.Fatal(err)
		}
		changed := config + "\n# another editor\n"
		if err := os.WriteFile(configPath, []byte(changed), 0644); err != nil {
			t.Fatal(err)
		}
		if err := plan.Run(); err == nil || len(*commands) != 0 || storage.ReadFileString(configPath) != changed {
			t.Fatalf("concurrent config overwritten: %v", err)
		}
	})
}

func TestBrokenOriginalSnapshotBlocksWritesAndCleanup(t *testing.T) {
	_, _, commands := originalFixture(t, "disabled")
	const initial = "# original\n"
	if err := os.WriteFile(configPath, []byte(initial), 0644); err != nil {
		t.Fatal(err)
	}
	if err := saveOriginalState(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(originalStore().OriginalPath(), []byte("broken json"), 0600); err != nil {
		t.Fatal(err)
	}
	config, _ := generate.Build([]int{22}, false, false)
	if err := configStore().Write(config); err == nil {
		t.Fatal("corrupt snapshot must prevent mutation")
	}
	if err := Cleanup(nil); err == nil || len(*commands) != 0 || storage.ReadFileString(configPath) != initial {
		t.Fatalf("corrupt snapshot cleanup must stop: %v", err)
	}
}

func TestCleanupDeleteFailurePreservesLegacyOwnershipForRetry(t *testing.T) {
	_, loaded, _ := originalFixture(t, "enabled")
	config, _ := generate.Build([]int{22}, false, false)
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatal(err)
	}
	*loaded = true
	output := commandOutput
	commandOutput = func(name string, args ...string) (string, error) {
		if name == "nft" && len(args) > 0 && args[0] == "delete" {
			return "denied", errors.New("delete failed")
		}
		return output(name, args...)
	}
	if err := Cleanup(nil); err == nil || !strings.Contains(err.Error(), "delete failed") {
		t.Fatalf("delete failure not reported: %v", err)
	}
	if !*loaded || storage.ReadFileString(configPath) != config {
		t.Fatal("failed rule deletion must keep config ownership for retry")
	}
	commandOutput = output
	if err := Cleanup(nil); err != nil || *loaded {
		t.Fatalf("cleanup retry failed: %v", err)
	}
}

func TestFirstSnapshotFailurePreventsConfigAndServiceMutations(t *testing.T) {
	for _, operation := range []string{"generate", "edit", "load", "autostart"} {
		t.Run(operation, func(t *testing.T) {
			_, _, commands := originalFixture(t, "enabled")
			const initial = "# original\n"
			if err := os.WriteFile(configPath, []byte(initial), 0644); err != nil {
				t.Fatal(err)
			}
			preserveOriginal = func() error { return errors.New("snapshot unavailable") }
			var err error
			switch operation {
			case "generate":
				config, _ := generate.Build([]int{22}, false, false)
				err = configStore().Write(config)
			case "edit":
				exists := commandExists
				commandExists = func(name string) bool { return name == "vim" || exists(name) }
				err = editConfig()
			case "load":
				err = configStore().Apply([]byte(initial))
			case "autostart":
				err = autostartService().Toggle(inputUI(t, "y\n"))
			}
			if err == nil || !strings.Contains(err.Error(), "snapshot unavailable") || len(*commands) != 0 || !bytes.Equal([]byte(storage.ReadFileString(configPath)), []byte(initial)) {
				t.Fatalf("mutation continued after snapshot failure: commands=%v err=%v", *commands, err)
			}
		})
	}
}

func TestConfirmedCleanupKeepsRecoveryBackups(t *testing.T) {
	state, loaded, _ := originalFixture(t, "disabled")
	const original = "# original file\n"
	if err := os.WriteFile(configPath, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	config, _ := generate.Build([]int{22}, false, false)
	if err := configStore().Write(config); err != nil {
		t.Fatal(err)
	}
	*loaded, *state = true, "enabled"
	if err := os.Chmod(configPath, 0644); err != nil {
		t.Fatal(err)
	}
	output := capturePortOutput(t, func() {
		if err := CleanupWithConfirm(inputUI(t, "y\n")); err != nil {
			t.Fatal(err)
		}
	})
	if *loaded || *state != "disabled" || storage.ReadFileString(configPath) != original {
		t.Fatalf("cleanup did not restore original state: loaded=%v boot=%s", *loaded, *state)
	}
	info, _ := os.Stat(configPath)
	if info.Mode().Perm() != 0600 {
		t.Fatal("cleanup did not restore original permissions")
	}
	if !strings.Contains(output, "清理配置") || !strings.Contains(output, "防火墙清理完成") {
		t.Fatalf("cleanup path/result missing: %s", output)
	}
	entries, err := (storage.BackupStore{Root: dataDir, Module: "firewall"}).List()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		data, err := os.ReadFile(entry.Path)
		if err != nil {
			t.Fatal(err)
		}
		found = found || string(data) == config
	}
	if !found {
		t.Fatal("cleanup did not preserve the configuration removed by cleanup")
	}
}

func TestCleanupServiceFailurePreservesConfigAndRules(t *testing.T) {
	state, loaded, _ := originalFixture(t, "disabled")
	config, _ := generate.Build([]int{22}, false, false)
	if err := configStore().Write(config); err != nil {
		t.Fatal(err)
	}
	*state, *loaded = "enabled", true
	commandRun = func(string, ...string) error { return errors.New("service failure") }
	if err := Cleanup(nil); err == nil || !strings.Contains(err.Error(), "service failure") {
		t.Fatalf("service failure not reported: %v", err)
	}
	if !*loaded || storage.ReadFileString(configPath) != config {
		t.Fatal("service restore failure should not remove config or rules")
	}
	if original, err := readOriginalState(); err != nil || original == nil {
		t.Fatal("service failure discarded recovery data")
	}
}

func TestCleanupRemovesOnlyManagedFirewallBlockAndSnailTable(t *testing.T) {
	setupTest(t)
	config, _ := generate.Build([]int{2222}, false, false)
	manual := "table inet custom { }\n"
	if err := os.WriteFile(configPath, []byte(config+"\n"+manual), 0600); err != nil {
		t.Fatal(err)
	}
	var mutations [][]string
	commandOutput = func(name string, args ...string) (string, error) {
		if name != "nft" {
			t.Fatal(name)
		}
		if reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
			if len(mutations) > 0 {
				return `{"nftables":[{"table":{"family":"inet","name":"custom"}}]}`, nil
			}
			return `{"nftables":[{"table":{"family":"inet","name":"snail"}},{"table":{"family":"inet","name":"custom"}}]}`, nil
		}
		mutations = append(mutations, args)
		return "", nil
	}
	if err := Cleanup(nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(mutations, [][]string{{"delete", "table", "inet", "snail"}}) {
		t.Fatalf("must never flush other tables: %v", mutations)
	}
	content := storage.ReadFileString(configPath)
	if strings.Contains(content, "table inet snail") || !strings.Contains(content, manual) {
		t.Fatalf("manual rules or managed removal incorrect: %s", content)
	}
}

func TestCleanupFirewallRefusesInvalidMarkersOrFailedInspection(t *testing.T) {
	for _, scenario := range []string{"broken marker", "query failure", "nft missing"} {
		t.Run(scenario, func(t *testing.T) {
			setupTest(t)
			content, _ := generate.Build([]int{22}, false, false)
			if scenario == "broken marker" {
				content = strings.Replace(content, configuration.ManagedEnd, "", 1)
			}
			if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			if scenario == "nft missing" {
				commandExists = func(string) bool { return false }
			}
			commandOutput = func(name string, args ...string) (string, error) {
				if scenario != "query failure" || !reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
					t.Fatalf("unexpected command: %s %v", name, args)
				}
				return "permission denied", errors.New("query failed")
			}
			if err := Cleanup(nil); err == nil || storage.ReadFileString(configPath) != content {
				t.Fatalf("unsafe cleanup changed configuration: %v", err)
			}
		})
	}
}
