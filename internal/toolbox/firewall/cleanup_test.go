package firewall

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/shared"
)

func TestCleanupRemovesOnlyManagedFirewallBlockAndSnailTable(t *testing.T) {
	setupTest(t)
	config, _ := buildConfig([]int{2222}, false, false)
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
	content := shared.ReadFileString(configPath)
	if strings.Contains(content, "table inet snail") || !strings.Contains(content, manual) {
		t.Fatalf("manual rules or managed removal incorrect: %s", content)
	}
}

func TestCleanupFirewallRefusesInvalidMarkersOrFailedInspection(t *testing.T) {
	for _, scenario := range []string{"broken marker", "query failure", "nft missing"} {
		t.Run(scenario, func(t *testing.T) {
			setupTest(t)
			content, _ := buildConfig([]int{22}, false, false)
			if scenario == "broken marker" {
				content = strings.Replace(content, configEnd, "", 1)
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
			if err := Cleanup(nil); err == nil || shared.ReadFileString(configPath) != content {
				t.Fatalf("unsafe cleanup changed configuration: %v", err)
			}
		})
	}
}
