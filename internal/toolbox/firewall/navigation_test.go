package firewall

import (
	"errors"
	"strings"
	"testing"

	"snail_tool/internal/shared"
)

func TestAccessRulePagesReturnOneLevelAtATime(t *testing.T) {
	for _, tc := range []struct {
		name, forward string
		backs         int
	}{
		{"port-value", "2\n1\n1\n1\n", 5},
		{"range-entry", "4\n1\n5\n1\n", 5},
		{"source-ports", "5\n1\n1\n192.0.2.1\n1\n443\n", 7},
		{"dual-stack-all-ports", "5\n1\n3\n192.0.2.1\n2001:db8::1\n2\n", 7},
		{"edit-rule", "5\n2\n2\n", 4},
		{"delete-by-id", "5\n3\n1\n", 4},
		{"delete-by-port", "5\n3\n2\n443\n", 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupTest(t)
			version := ruleTestConfig(t, orderTestRules())
			view := inputUI(t, tc.forward+strings.Repeat("q\n", tc.backs)+"parent-next\n")
			if _, err := editAccessRulesDraft(view, string(version.content), false); !errors.Is(err, shared.ErrReturnToMenu) {
				t.Fatalf("back did not leave the first page: %v", err)
			}
			if next, err := view.Ask("parent"); err != nil || next != "parent-next" {
				t.Fatalf("back skipped a page or consumed parent input: next=%q err=%v", next, err)
			}
			if shared.ReadFileString(configPath) != string(version.content) {
				t.Fatal("back navigation changed the firewall configuration")
			}
		})
	}
}

func TestSourceRuleCanChangeBranchAfterReturning(t *testing.T) {
	// Return from ports to access scope, select all ports, then return from
	// protocol. The skipped ports page must not reappear in the back history.
	view := inputUI(t, "1\n192.0.2.1\n1\nq\n2\nq\nq\nq\nq\nparent-next\n")
	if _, err := askAllowRule(view, allowRule{ID: "aaaaaaaa"}); !errors.Is(err, shared.ErrReturnToMenu) {
		t.Fatal(err)
	}
	if next, err := view.Ask("parent"); err != nil || next != "parent-next" {
		t.Fatalf("branch change left a stale page in history: next=%q err=%v", next, err)
	}
}
