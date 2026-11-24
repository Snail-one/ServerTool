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
		{"range-entry", "l\n1\n5\n1\n", 5},
		{"source-ports", "5\nn\n\n1\n192.0.2.1\n\n\n\n\n443\n", 11},
		{"dual-stack-all-ports", "5\nn\n\n3\n192.0.2.1\n2001:db8::1\n\n\n\n\n\n", 12},
		{"edit-rule", "5\n2\n1\n", 4},
		{"rule-details-by-id", "5\naaaaaaaa\n", 3},
		{"delete-by-port", "5\nf\n443\n", 4},
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

func TestDockerSettingsOpenFromForwardRuleManager(t *testing.T) {
	setupTest(t)
	version := ruleTestConfig(t, nil)
	view := inputUI(t, "6\nq\n7\nq\nq\nparent-next\n")
	output := capturePortOutput(t, func() {
		if err := runForwardRuleManager(view); !errors.Is(err, shared.ErrReturnToMenu) {
			t.Fatal(err)
		}
	})
	for _, name := range []string{"Docker 流量放行", "Docker 放行标记"} {
		if !strings.Contains(output, "转发规则管理  ›  "+name) {
			t.Fatalf("%s did not open under the forward rule manager:\n%s", name, output)
		}
	}
	if next, err := view.ReadLine(); err != nil || next != "parent-next" {
		t.Fatalf("Docker menu navigation consumed parent input: %q %v", next, err)
	}
	if shared.ReadFileString(configPath) != string(version.content) {
		t.Fatal("opening Docker settings changed the firewall configuration")
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

func TestRuleDraftShowsDraftActionsOnlyWithChanges(t *testing.T) {
	setupTest(t)
	version := ruleTestConfig(t, nil)
	output := capturePortOutput(t, func() {
		view := inputUI(t, "3\n1\n1\n1\n8443\nr\nq\n")
		if _, err := editAccessRulesDraft(view, string(version.content), false); !errors.Is(err, shared.ErrReturnToMenu) {
			t.Fatal(err)
		}
	})
	before, rest, found := strings.Cut(output, "已更新草稿")
	if !found {
		t.Fatal("expected an updated draft")
	}
	edited, restored, found := strings.Cut(rest, "已还原本次全部修改")
	if !found {
		t.Fatal("expected the draft to be restored")
	}
	for _, label := range []string{"保存并立即生效", "还原本次草稿", "放弃草稿并返回"} {
		if strings.Contains(before, label) || strings.Count(edited, label) != 1 || strings.Contains(restored, label) {
			t.Fatalf("%q should appear only with unsaved changes:\n%s", label, output)
		}
	}
	const warning = "⚠ 有未保存的修改。"
	if strings.Contains(before, warning) || strings.Contains(restored, warning) ||
		!strings.Contains(edited, warning+"输入 s 保存并应用。\n\n❯ 请选择：") {
		t.Fatalf("the unsaved warning should appear one blank line above the menu prompt only with changes:\n%s", output)
	}
	if !strings.Contains(before, "0/q 返回防火墙管理") || !strings.Contains(restored, "0/q 返回防火墙管理") {
		t.Fatal("a clean draft should offer to return to the firewall menu")
	}
}
