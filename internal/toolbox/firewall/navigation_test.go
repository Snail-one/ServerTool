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
		{"source-ports", "5\n1\n\n1\n192.0.2.1\n\n\n\n\n443\n", 11},
		{"dual-stack-all-ports", "5\n1\n\n3\n192.0.2.1\n2001:db8::1\n\n\n\n\n\n", 12},
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
	// Leave the IPv6 source page, switch to IPv4, then return from protocol.
	// The skipped IPv6 page must not reappear in the back history.
	view := inputUI(t, "\n3\n192.0.2.1\nq\nq\n1\n192.0.2.1\n\n\n\n\n\nq\nq\nq\nq\nq\nq\nq\nq\nq\nparent-next\n")
	if _, err := askAllowRule(view, allowRule{ID: "aaaaaaaa"}); !errors.Is(err, shared.ErrReturnToMenu) {
		t.Fatal(err)
	}
	if next, err := view.Ask("parent"); err != nil || next != "parent-next" {
		t.Fatalf("branch change left a stale page in history: next=%q err=%v", next, err)
	}
}

func TestRuleDraftDefaultsToKeepingUnsavedChanges(t *testing.T) {
	for _, forward := range []bool{false, true} {
		t.Run(map[bool]string{false: "access", true: "forward"}[forward], func(t *testing.T) {
			setupTest(t)
			version := ruleTestConfig(t, nil)
			if forward {
				defer useForwardSession()()
			}
			view := inputUI(t, "3\n1\n1\n1\n8443\n0\n\nq\nq\ns\nparent-next\n")
			var draft string
			output := capturePortOutput(t, func() {
				var err error
				draft, err = editAccessRulesDraft(view, string(version.content), false)
				if err != nil {
					t.Fatal(err)
				}
			})
			if strings.Count(output, "还有未保存的配置，是否放弃修改并返回？(y/N)") != 2 || !strings.Contains(draft, "8443") {
				t.Fatal("declining either prompt should retain the edited draft")
			}
			if shared.ReadFileString(configPath) != string(version.content) {
				t.Fatal("closing the confirmation must never write the draft")
			}
			if next, err := view.ReadLine(); err != nil || next != "parent-next" {
				t.Fatalf("draft navigation consumed parent input: %q %v", next, err)
			}
		})
	}
}
