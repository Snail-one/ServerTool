package firewall

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/shared"
)

func orderTestRules() []allowRule {
	return []allowRule{
		{ID: "aaaaaaaa", Kind: "port", Ports: "443", Protocol: "both"},
		{ID: "bbbbbbbb", Kind: "source_port", Source: "192.168.1.0/24", Ports: "443", Protocol: "tcp"},
		{ID: "cccccccc", Kind: "port", Ports: "53", Protocol: "udp"},
		{ID: "dddddddd", Kind: "port", Ports: "8080", Protocol: "tcp"},
	}
}

func TestReorderRulesPersistsMatchingOrderAndKeepsLogicalRulesIntact(t *testing.T) {
	for _, scenario := range []struct {
		name, input string
		order       []int
	}{
		{"up", "2\nu\ns\n", []int{1, 0, 2, 3}},
		{"down", "BBBBBBBB\nD\ns\n", []int{0, 2, 1, 3}},
		{"top", "4\nt\ns\n", []int{3, 0, 1, 2}},
		{"bottom", "aaaaaaaa\nb\ns\n", []int{1, 2, 3, 0}},
		{"multiple moves and reset", "2\nu\n4\nt\nr\n3\nb\ns\n", []int{0, 1, 3, 2}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			setupTest(t)
			rules := orderTestRules()
			version := ruleTestConfig(t, rules)
			manual := "        ip saddr 192.0.2.1 tcp dport 8443 counter accept\n"
			content := strings.Replace(string(version.content), rulesEnd+"\n", rulesEnd+"\n"+manual, 1)
			if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			calls := acceptRuleCommands(t)
			if err := runAccessRuleManager(inputUI(t, "5\n4\n"+scenario.input+"\nq\ns\ny\n")); err != nil {
				t.Fatal(err)
			}
			var want []allowRule
			for _, index := range scenario.order {
				want = append(want, rules[index])
			}
			saved := shared.ReadFileString(configPath)
			got, err := managedRules(saved)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("wrong saved matching order: got=%v want=%v err=%v", got, want, err)
			}
			if !strings.Contains(saved, manual) || !strings.Contains(saved, "define snail_ssh_port = { 2222 }") {
				t.Fatal("reordering changed manual or baseline configuration")
			}
			if !reflect.DeepEqual(*calls, []string{"check", "load"}) {
				t.Fatalf("draft moves must result in only one validation and load: %v", *calls)
			}
			backups, err := filepath.Glob(filepath.Join(firewallBackupDir(), "nftables.conf.bak.*"))
			if err != nil || len(backups) != 1 || shared.ReadFileString(backups[0]) != content {
				t.Fatal("reordering must back up the original order exactly once")
			}
			// The serializer emits both protocols consecutively under the same ID.
			both := strings.Join(rules[0].nftLines(), "\n        ")
			if !strings.Contains(saved, both) {
				t.Fatal("TCP+UDP rule was split or modified during movement")
			}
			if !reflect.DeepEqual(rules, orderTestRules()) {
				t.Fatal("draft editing mutated the input snapshot")
			}
		})
	}
}

func TestReorderDraftCancellationAndBoundariesDoNotWriteOrPause(t *testing.T) {
	for _, input := range []string{
		"q\n", "2\nu\n返回\n",
		"u\nt\ns\nq\n", "4\nd\nb\ns\nq\n", "2\nu\nr\ns\nq\n",
		"bad choice\n2\nu\nq\n",
	} {
		t.Run(input, func(t *testing.T) {
			setupTest(t)
			rules := orderTestRules()
			version := ruleTestConfig(t, rules)
			view := inputUI(t, input+"next\n")
			if _, err := reorderAllowRulesDraft(view, rules); !errors.Is(err, shared.ErrReturnToMenu) {
				t.Fatalf("draft cancellation did not return: %v", err)
			}
			if got := shared.ReadFileString(configPath); got != string(version.content) {
				t.Fatal("unsaved or unchanged draft altered configuration")
			}
			entries, err := (shared.BackupStore{Root: dataDir, Module: "firewall"}).List()
			if err != nil || len(entries) != 0 {
				t.Fatalf("unsaved draft created a backup: %v %v", entries, err)
			}
			if next, err := view.Ask("下一项："); err != nil || next != "next" {
				t.Fatalf("return consumed extra input: next=%q err=%v", next, err)
			}
		})
	}
}

func TestReorderRejectsConcurrentConfigChanges(t *testing.T) {
	setupTest(t)
	rules := orderTestRules()
	version := ruleTestConfig(t, rules)
	changed := string(version.content) + "\n# external edit\n"
	calls := acceptRuleCommands(t)
	query := commandOutput
	commandOutput = func(name string, args ...string) (string, error) {
		if len(args) == 3 && args[0] == "-c" {
			if err := os.WriteFile(configPath, []byte(changed), 0600); err != nil {
				t.Fatal(err)
			}
		}
		return query(name, args...)
	}
	if err := runAccessRuleManager(inputUI(t, "5\n4\n2\nu\ns\n\nq\ns\ny\n")); err == nil || errors.Is(err, shared.ErrReturnToMenu) {
		t.Fatalf("concurrent edit should prevent saving: %v", err)
	}
	if shared.ReadFileString(configPath) != changed || !reflect.DeepEqual(*calls, []string{"check"}) {
		t.Fatalf("reorder overwrote or loaded concurrent edits: %v", *calls)
	}
}

func TestRuleManagerOpensOrderEditorAndSelectsNumericRuleID(t *testing.T) {
	setupTest(t)
	rules := orderTestRules()
	rules[2].ID = "00000001"
	ruleTestConfig(t, rules)
	acceptRuleCommands(t)
	// Enter sorting, select the numeric ID at position 3, put it first, save,
	// read the result once, and return from the rule manager.
	if err := runAccessRuleManager(inputUI(t, "5\n4\n00000001\nt\ns\n\nq\ns\ny\n")); err != nil {
		t.Fatal(err)
	}
	got, err := managedRules(shared.ReadFileString(configPath))
	if err != nil || !reflect.DeepEqual(got, []allowRule{rules[2], rules[0], rules[1], rules[3]}) {
		t.Fatalf("numeric ID was mistaken for a position: %v %v", got, err)
	}
}
