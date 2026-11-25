package access_test

import (
	"errors"
	"strings"
	"testing"

	rulespkg "snail_tool/internal/firewall/internal/rules"
	"snail_tool/internal/firewall/internal/testenv"
	"snail_tool/internal/storage"
	"snail_tool/internal/ui"
)

func TestAccessRulePagesReturnOneLevelAtATime(t *testing.T) {
	for _, tc := range []struct {
		name, forward string
		backs         int
	}{
		{"port-value", "2\n1\n1\n1\n", 5},
		{"range-entry", "l\n1\n5\n1\n", 5},
		{"source-ports", "5\nn\n\n1\n192.0.2.1\n\n\n\n\n443\n", 11},
		{"dual-stack-all-ports", "5\nn\n\n3\n192.0.2.1\n2001:db8::1\n\n\n\n\n\n", 12},
		{"edit-rule", "5\n2\n1\n", 4},
		{"rule-details-by-id", "5\naaaaaaaa\n", 3},
		{"delete-by-port", "5\nf\n443\n", 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := testenv.New(t)
			version := env.RuleConfig(t, testenv.OrderRules())
			view := testenv.Input(t, tc.forward+strings.Repeat("q\n", tc.backs)+"parent-next\n")
			if _, err := env.AccessPolicy().EditPolicyDraft(view, string(version.Content), false, nil); !errors.Is(err, ui.ErrReturnToMenu) {
				t.Fatalf("back did not leave the first page: %v", err)
			}
			if next, err := view.Ask("parent"); err != nil || next != "parent-next" {
				t.Fatalf("back skipped a page or consumed parent input: next=%q err=%v", next, err)
			}
			if storage.ReadFileString(env.ConfigPath) != string(version.Content) {
				t.Fatal("back navigation changed the firewall configuration")
			}
		})
	}
}

func TestSourceRuleCanChangeBranchAfterReturning(t *testing.T) {
	env := testenv.New(t)
	// Leave the IPv6 source page, switch to IPv4, then return from protocol.
	// The skipped IPv6 page must not reappear in the back history.
	view := testenv.Input(t, "\n3\n192.0.2.1\nq\nq\n1\n192.0.2.1\n\n\n\n\n\nq\nq\nq\nq\nq\nq\nq\nq\nq\nparent-next\n")
	if _, err := env.AccessPolicy().AskAllowRule(view, rulespkg.Rule{ID: "aaaaaaaa"}); !errors.Is(err, ui.ErrReturnToMenu) {
		t.Fatal(err)
	}
	if next, err := view.Ask("parent"); err != nil || next != "parent-next" {
		t.Fatalf("branch change left a stale page in history: next=%q err=%v", next, err)
	}
}
