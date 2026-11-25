package restore_test

import (
	"bytes"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/firewall/configuration/generate"
	"snail_tool/internal/firewall/internal/testenv"
	"snail_tool/internal/storage"
)

func TestCleanupRestoresFirstConfigAndAutostartWithoutFlushingOtherTables(t *testing.T) {
	for _, exists := range []bool{true, false} {
		for _, boot := range []string{"disabled", "enabled", "enabled-runtime"} {
			name := boot + "/absent"
			if exists {
				name = boot + "/existing"
			}
			t.Run(name, func(t *testing.T) {
				env, state, loaded, commands := testenv.Original(t, boot)
				const initial = "# pre-tool configuration\ntable inet custom { }\n"
				if exists {
					if err := os.WriteFile(env.ConfigPath, []byte(initial), 0600); err != nil {
						t.Fatal(err)
					}
				}
				config, _ := generate.Build([]int{2222}, false, false)
				if err := env.Store().Write(config); err != nil {
					t.Fatal(err)
				}
				first, err := env.ReadOriginal()
				if err != nil || first == nil || first.Autostart != boot || (first.Config != nil) != exists {
					t.Fatalf("missing pre-mutation snapshot: %+v %v", first, err)
				}
				if err := env.Store().Apply([]byte(config)); err != nil {
					t.Fatal(err)
				}
				// Later changes must never replace the first snapshot.
				*state = "enabled-runtime"
				if boot == "enabled" {
					*state = "disabled"
				} else if boot == "enabled-runtime" {
					*state = "enabled"
				}
				if err := env.Store().Write(config + "\n# later change\n"); err != nil {
					t.Fatal(err)
				}
				later, err := env.ReadOriginal()
				if err != nil || !reflect.DeepEqual(first, later) {
					t.Fatalf("first snapshot replaced: %v", err)
				}
				if err := env.Restore().Run(); err != nil {
					t.Fatal(err)
				}
				data, present, err := storage.ReadRegularFile(env.ConfigPath)
				if err != nil || present != exists || exists && string(data) != initial {
					t.Fatalf("original config not restored: present=%v data=%q err=%v", present, data, err)
				}
				if exists {
					info, _ := os.Stat(env.ConfigPath)
					if info.Mode().Perm() != 0600 {
						t.Fatal("original permissions not restored")
					}
				}
				if *loaded || *state != boot {
					t.Fatalf("runtime/boot state not restored: loaded=%v boot=%s commands=%v", *loaded, *state, *commands)
				}
				if err := env.Restore().Run(); err != nil {
					t.Fatalf("cleanup must be repeatable: %v", err)
				}
				if original, err := env.ReadOriginal(); err != nil || original == nil {
					t.Fatal("cleanup removed the recovery snapshot")
				}
			})
		}
	}
}

func TestCleanupCancellationAndConcurrentEditNeverMutate(t *testing.T) {
	for _, answer := range []string{"n\n", "\n", ""} {
		t.Run("answer="+answer, func(t *testing.T) {
			env, _, _, commands := testenv.Original(t, "disabled")
			config, _ := generate.Build([]int{22}, false, false)
			if err := os.WriteFile(env.ConfigPath, []byte(config), 0644); err != nil {
				t.Fatal(err)
			}
			err := env.Restore().RunWithConfirm(testenv.Input(t, answer))
			if err == nil || len(*commands) != 0 || storage.ReadFileString(env.ConfigPath) != config {
				t.Fatalf("missing/refused confirmation changed state: commands=%v err=%v", *commands, err)
			}
			entries, _ := (storage.BackupStore{Root: env.DataDir, Module: "firewall"}).List()
			if len(entries) != 0 {
				t.Fatal("cancellation must not create backups or first snapshots")
			}
		})
	}
	t.Run("concurrent edit", func(t *testing.T) {
		env, _, _, commands := testenv.Original(t, "disabled")
		config, _ := generate.Build([]int{22}, false, false)
		if err := os.WriteFile(env.ConfigPath, []byte(config), 0644); err != nil {
			t.Fatal(err)
		}
		plan, err := env.Restore().Prepare()
		if err != nil {
			t.Fatal(err)
		}
		changed := config + "\n# another editor\n"
		if err := os.WriteFile(env.ConfigPath, []byte(changed), 0644); err != nil {
			t.Fatal(err)
		}
		if err := plan.Run(); err == nil || len(*commands) != 0 || storage.ReadFileString(env.ConfigPath) != changed {
			t.Fatalf("concurrent config overwritten: %v", err)
		}
	})
}

func TestBrokenOriginalSnapshotBlocksWritesAndCleanup(t *testing.T) {
	env, _, _, commands := testenv.Original(t, "disabled")
	const initial = "# original\n"
	if err := os.WriteFile(env.ConfigPath, []byte(initial), 0644); err != nil {
		t.Fatal(err)
	}
	if err := env.SaveOriginal(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env.Store().OriginalPath(), []byte("broken json"), 0600); err != nil {
		t.Fatal(err)
	}
	config, _ := generate.Build([]int{22}, false, false)
	if err := env.Store().Write(config); err == nil {
		t.Fatal("corrupt snapshot must prevent mutation")
	}
	if err := env.Restore().Run(); err == nil || len(*commands) != 0 || storage.ReadFileString(env.ConfigPath) != initial {
		t.Fatalf("corrupt snapshot cleanup must stop: %v", err)
	}
}

func TestCleanupDeleteFailurePreservesLegacyOwnershipForRetry(t *testing.T) {
	env, _, loaded, _ := testenv.Original(t, "enabled")
	config, _ := generate.Build([]int{22}, false, false)
	if err := os.WriteFile(env.ConfigPath, []byte(config), 0644); err != nil {
		t.Fatal(err)
	}
	*loaded = true
	output := env.CommandOutput
	env.CommandOutput = func(name string, args ...string) (string, error) {
		if name == "nft" && len(args) > 0 && args[0] == "delete" {
			return "denied", errors.New("delete failed")
		}
		return output(name, args...)
	}
	if err := env.Restore().Run(); err == nil || !strings.Contains(err.Error(), "delete failed") {
		t.Fatalf("delete failure not reported: %v", err)
	}
	if !*loaded || storage.ReadFileString(env.ConfigPath) != config {
		t.Fatal("failed rule deletion must keep config ownership for retry")
	}
	env.CommandOutput = output
	if err := env.Restore().Run(); err != nil || *loaded {
		t.Fatalf("cleanup retry failed: %v", err)
	}
}

func TestFirstSnapshotFailurePreventsConfigAndServiceMutations(t *testing.T) {
	for _, operation := range []string{"generate", "edit", "load", "autostart"} {
		t.Run(operation, func(t *testing.T) {
			env, _, _, commands := testenv.Original(t, "enabled")
			const initial = "# original\n"
			if err := os.WriteFile(env.ConfigPath, []byte(initial), 0644); err != nil {
				t.Fatal(err)
			}
			env.PreserveOriginal = func() error { return errors.New("snapshot unavailable") }
			var err error
			switch operation {
			case "generate":
				config, _ := generate.Build([]int{22}, false, false)
				err = env.Store().Write(config)
			case "edit":
				exists := env.CommandExists
				env.CommandExists = func(name string) bool { return name == "vim" || exists(name) }
				err = env.EditConfig()
			case "load":
				err = env.Store().Apply([]byte(initial))
			case "autostart":
				err = env.Autostart().Toggle(testenv.Input(t, "y\n"))
			}
			if err == nil || !strings.Contains(err.Error(), "snapshot unavailable") || len(*commands) != 0 || !bytes.Equal([]byte(storage.ReadFileString(env.ConfigPath)), []byte(initial)) {
				t.Fatalf("mutation continued after snapshot failure: commands=%v err=%v", *commands, err)
			}
		})
	}
}

func TestConfirmedCleanupKeepsRecoveryBackups(t *testing.T) {
	env, state, loaded, _ := testenv.Original(t, "disabled")
	const original = "# original file\n"
	if err := os.WriteFile(env.ConfigPath, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	config, _ := generate.Build([]int{22}, false, false)
	if err := env.Store().Write(config); err != nil {
		t.Fatal(err)
	}
	*loaded, *state = true, "enabled"
	if err := os.Chmod(env.ConfigPath, 0644); err != nil {
		t.Fatal(err)
	}
	output := testenv.CaptureOutput(t, func() {
		if err := env.Restore().RunWithConfirm(testenv.Input(t, "y\n")); err != nil {
			t.Fatal(err)
		}
	})
	if *loaded || *state != "disabled" || storage.ReadFileString(env.ConfigPath) != original {
		t.Fatalf("cleanup did not restore original state: loaded=%v boot=%s", *loaded, *state)
	}
	info, _ := os.Stat(env.ConfigPath)
	if info.Mode().Perm() != 0600 {
		t.Fatal("cleanup did not restore original permissions")
	}
	if !strings.Contains(output, "清理配置") || !strings.Contains(output, "防火墙清理完成") {
		t.Fatalf("cleanup path/result missing: %s", output)
	}
	entries, err := (storage.BackupStore{Root: env.DataDir, Module: "firewall"}).List()
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
	env, state, loaded, _ := testenv.Original(t, "disabled")
	config, _ := generate.Build([]int{22}, false, false)
	if err := env.Store().Write(config); err != nil {
		t.Fatal(err)
	}
	*state, *loaded = "enabled", true
	env.CommandRun = func(string, ...string) error { return errors.New("service failure") }
	if err := env.Restore().Run(); err == nil || !strings.Contains(err.Error(), "service failure") {
		t.Fatalf("service failure not reported: %v", err)
	}
	if !*loaded || storage.ReadFileString(env.ConfigPath) != config {
		t.Fatal("service restore failure should not remove config or rules")
	}
	if original, err := env.ReadOriginal(); err != nil || original == nil {
		t.Fatal("service failure discarded recovery data")
	}
}

func TestCleanupRemovesOnlyManagedFirewallBlockAndSnailTable(t *testing.T) {
	env := testenv.New(t)
	config, _ := generate.Build([]int{2222}, false, false)
	manual := "table inet custom { }\n"
	if err := os.WriteFile(env.ConfigPath, []byte(config+"\n"+manual), 0600); err != nil {
		t.Fatal(err)
	}
	var mutations [][]string
	env.CommandOutput = func(name string, args ...string) (string, error) {
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
	if err := env.Restore().Run(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(mutations, [][]string{{"delete", "table", "inet", "snail"}}) {
		t.Fatalf("must never flush other tables: %v", mutations)
	}
	content := storage.ReadFileString(env.ConfigPath)
	if strings.Contains(content, "table inet snail") || !strings.Contains(content, manual) {
		t.Fatalf("manual rules or managed removal incorrect: %s", content)
	}
}

func TestCleanupFirewallRefusesInvalidMarkersOrFailedInspection(t *testing.T) {
	for _, scenario := range []string{"broken marker", "query failure", "nft missing"} {
		t.Run(scenario, func(t *testing.T) {
			env := testenv.New(t)
			content, _ := generate.Build([]int{22}, false, false)
			if scenario == "broken marker" {
				content = strings.Replace(content, configuration.ManagedEnd, "", 1)
			}
			if err := os.WriteFile(env.ConfigPath, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			if scenario == "nft missing" {
				env.CommandExists = func(string) bool { return false }
			}
			env.CommandOutput = func(name string, args ...string) (string, error) {
				if scenario != "query failure" || !reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
					t.Fatalf("unexpected command: %s %v", name, args)
				}
				return "permission denied", errors.New("query failed")
			}
			if err := env.Restore().Run(); err == nil || storage.ReadFileString(env.ConfigPath) != content {
				t.Fatalf("unsafe cleanup changed configuration: %v", err)
			}
		})
	}
}
