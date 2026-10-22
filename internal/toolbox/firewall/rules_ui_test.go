package firewall

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/shared"
)

func acceptRuleCommands(t *testing.T) *[]string {
	t.Helper()
	var calls []string
	commandOutput = func(name string, args ...string) (string, error) {
		if name != "nft" {
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		if len(args) == 3 && args[0] == "-c" && args[1] == "-f" {
			calls = append(calls, "check")
			if _, err := managedRules(shared.ReadFileString(args[2])); err != nil {
				t.Fatalf("invalid rule block: %v", err)
			}
			return "", nil
		}
		if reflect.DeepEqual(args, []string{"-f", configPath}) {
			calls = append(calls, "load")
			return "", nil
		}
		t.Fatalf("unexpected nft arguments: %v", args)
		return "", nil
	}
	return &calls
}

func TestRuleManagerAddsOneLogicalRuleForBothProtocols(t *testing.T) {
	setupTest(t)
	ruleTestConfig(t, nil)
	calls := acceptRuleCommands(t)
	// Main menu -> add -> port -> both IP versions -> range -> both protocols.
	if err := runRuleManager(inputUI(t, "1\n1\n3\n8000-8100\n3\ny\n\nq\n")); !errors.Is(err, shared.ErrReturnToMenu) {
		t.Fatal(err)
	}
	rules, err := managedRules(shared.ReadFileString(configPath))
	if err != nil || len(rules) != 1 || rules[0].Kind != "port" || rules[0].Family != "both" || rules[0].Ports != "8000-8100" || rules[0].Protocol != "both" {
		t.Fatalf("incorrect saved rule: %v %v", rules, err)
	}
	if !reflect.DeepEqual(*calls, []string{"check", "load"}) {
		t.Fatalf("rule must validate and load once: %v", *calls)
	}
}

func TestEditRuleKeepsIDAndCanonicalizesCIDR(t *testing.T) {
	setupTest(t)
	initial := []allowRule{{ID: "aaaaaaaa", Kind: "source_port", Source: "192.168.1.0/24", Ports: "443", Protocol: "tcp"}}
	version := ruleTestConfig(t, initial)
	acceptRuleCommands(t)
	if err := changeAllowRule(inputUI(t, "\n\n192.168.1.1/16\n21069\n3\ny\n"), version, initial, "aaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	rules, err := managedRules(shared.ReadFileString(configPath))
	want := []allowRule{{ID: "aaaaaaaa", Kind: "source_port", Family: "ipv4", Source: "192.168.0.0/16", Ports: "21069", Protocol: "both"}}
	if err != nil || !reflect.DeepEqual(rules, want) {
		t.Fatalf("editing changed ID or failed to normalize: %v %v", rules, err)
	}
}

func TestRuleManagerEditsByDisplayedNumberAndKeepsOtherRules(t *testing.T) {
	setupTest(t)
	rules := orderTestRules()
	ruleTestConfig(t, rules)
	calls := acceptRuleCommands(t)
	// Edit row 2, keep its type/source, and change its ports/protocol.
	if err := runRuleManager(inputUI(t, "2\n2\n\n\n\n8443\n3\ny\n\nq\n")); !errors.Is(err, shared.ErrReturnToMenu) {
		t.Fatal(err)
	}
	want := append([]allowRule(nil), rules...)
	want[1].Ports, want[1].Protocol = "8443", "both"
	want[1].Family = "ipv4"
	got, err := managedRules(shared.ReadFileString(configPath))
	if err != nil || !reflect.DeepEqual(got, want) || !reflect.DeepEqual(*calls, []string{"check", "load"}) {
		t.Fatalf("editing by number changed the wrong rule: got=%v err=%v calls=%v", got, err, *calls)
	}
}

func TestChooseRuleAcceptsNumbersAndPrefersFullNumericIDs(t *testing.T) {
	rules := []allowRule{{ID: "aaaaaaaa"}, {ID: "00000001"}}
	for _, scenario := range []struct{ input, want string }{
		{"1\n", "aaaaaaaa"},
		{"2\n", "00000001"},
		{"00000001\n", "00000001"},
		{"AAAAAAAA\n", "aaaaaaaa"},
		{"3\nno-such-id\n-1\n2\n", "00000001"},
	} {
		t.Run(scenario.input, func(t *testing.T) {
			view := inputUI(t, scenario.input+"next\n")
			id, err := chooseRuleID(view, rules)
			if err != nil || id != scenario.want {
				t.Fatalf("id=%q want=%q err=%v", id, scenario.want, err)
			}
			if next, err := view.Ask("next"); err != nil || next != "next" {
				t.Fatal("selection consumed extra input")
			}
		})
	}
}

func TestDeleteByNumberUsesTheDisplayedFilteredList(t *testing.T) {
	setupTest(t)
	rules := []allowRule{
		{ID: "aaaaaaaa", Kind: "port", Ports: "443", Protocol: "tcp"},
		{ID: "bbbbbbbb", Kind: "port", Ports: "8080", Protocol: "tcp"},
		{ID: "cccccccc", Kind: "port", Ports: "443", Protocol: "udp"},
	}
	version := ruleTestConfig(t, rules)
	acceptRuleCommands(t)
	// The second matching row is rule C, which was third in the full list.
	if err := deleteAllowRule(inputUI(t, "2\n443\n2\ny\n"), version, rules); err != nil {
		t.Fatal(err)
	}
	got, err := managedRules(shared.ReadFileString(configPath))
	if err != nil || !reflect.DeepEqual(got, rules[:2]) {
		t.Fatalf("filtered number selected a rule from the original list: got=%v err=%v", got, err)
	}
}

func TestRuleAddCancellationAndDuplicateDoNotChangeConfiguration(t *testing.T) {
	for _, scenario := range []string{"cancel", "duplicate", "cancel draft", "cancel family", "cancel second source"} {
		t.Run(scenario, func(t *testing.T) {
			setupTest(t)
			rules := []allowRule{{ID: "aaaaaaaa", Kind: "source_port", Source: "192.168.0.0/16", Ports: "443", Protocol: "tcp"}}
			version := ruleTestConfig(t, rules)
			input := "1\n1\n8443\n1\nn\n"
			if scenario == "duplicate" {
				input = "3\n1\n192.168.1.1/16\n443-443\n1\n"
			} else if scenario == "cancel draft" {
				input = "q\n"
			} else if scenario == "cancel family" {
				input = "1\nq\n"
			} else if scenario == "cancel second source" {
				input = "3\n3\n192.168.1.0/24\nq\n"
			}
			err := changeAllowRule(inputUI(t, input), version, rules, "")
			if scenario == "duplicate" && (err == nil || !strings.Contains(err.Error(), "完全重复")) {
				t.Fatalf("normalized duplicate not rejected: %v", err)
			}
			if shared.ReadFileString(configPath) != string(version.content) {
				t.Fatal("cancelled or duplicate addition changed the file")
			}
		})
	}
}

func TestQueryPortAndDeleteOnlyChosenIDPreservingOtherSources(t *testing.T) {
	setupTest(t)
	rules := []allowRule{
		{ID: "aaaaaaaa", Kind: "port", Ports: "443", Protocol: "tcp"},
		{ID: "bbbbbbbb", Kind: "source_port", Source: "192.168.1.0/24", Ports: "443", Protocol: "tcp"},
		{ID: "cccccccc", Kind: "source", Source: "10.0.0.0/8", Protocol: "both"},
		{ID: "dddddddd", Kind: "port", Ports: "8000-8100", Protocol: "udp"},
	}
	version := ruleTestConfig(t, rules)
	// All-port source rules are included because they also allow the queried port.
	matches, err := queryRulesByPort(inputUI(t, "443\n"), rules)
	if err != nil || !reflect.DeepEqual(matches, rules[:3]) {
		t.Fatalf("query must list each matching rule independently: %v %v", matches, err)
	}
	rangeMatches, err := queryRulesByPort(inputUI(t, "8080\n"), rules)
	if err != nil || len(rangeMatches) != 2 || rangeMatches[1].ID != "dddddddd" {
		t.Fatalf("port inside range not matched: %v %v", rangeMatches, err)
	}
	acceptRuleCommands(t)
	// dddddddd is not in the query result, so it must not be selected/deleted.
	if err := deleteAllowRule(inputUI(t, "2\n443\ndddddddd\nbbbbbbbb\ny\n"), version, rules); err != nil {
		t.Fatal(err)
	}
	got, err := managedRules(shared.ReadFileString(configPath))
	want := []allowRule{rules[0], rules[2], rules[3]}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("deleting by port removed unrelated/global/source rules: %v %v", got, err)
	}
}

func TestDeleteByIDRequiresConfirmation(t *testing.T) {
	setupTest(t)
	rules := []allowRule{{ID: "aaaaaaaa", Kind: "port", Ports: "443", Protocol: "tcp"}}
	version := ruleTestConfig(t, rules)
	if err := deleteAllowRule(inputUI(t, "1\naaaaaaaa\nn\n"), version, rules); !errors.Is(err, shared.ErrReturnToMenu) {
		t.Fatal(err)
	}
	if shared.ReadFileString(configPath) != string(version.content) {
		t.Fatal("cancelled deletion changed configuration")
	}
}
