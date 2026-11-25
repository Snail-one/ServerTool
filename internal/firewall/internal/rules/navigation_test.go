package rules_test

import (
	"errors"
	"strings"
	"testing"

	"snail_tool/internal/firewall/internal/testenv"
	"snail_tool/internal/storage"
	"snail_tool/internal/ui"
)

func TestRuleDraftDefaultsToKeepingUnsavedChanges(t *testing.T) {
	for _, forward := range []bool{false, true} {
		t.Run(map[bool]string{false: "access", true: "forward"}[forward], func(t *testing.T) {
			env := testenv.New(t)
			version := env.RuleConfig(t, nil)
			session := env.AccessPolicy
			if forward {
				session = env.ForwardPolicy
			}
			view := testenv.Input(t, "3\n1\n1\n1\n8443\n0\n\nq\nq\ns\nparent-next\n")
			var draft string
			output := testenv.CaptureOutput(t, func() {
				var err error
				draft, err = session().EditPolicyDraft(view, string(version.Content), false, nil)
				if err != nil {
					t.Fatal(err)
				}
			})
			if strings.Count(output, "还有未保存的配置，是否放弃修改并返回？(y/N)") != 2 || !strings.Contains(draft, "8443") {
				t.Fatal("declining either prompt should retain the edited draft")
			}
			if storage.ReadFileString(env.ConfigPath) != string(version.Content) {
				t.Fatal("closing the confirmation must never write the draft")
			}
			if next, err := view.ReadLine(); err != nil || next != "parent-next" {
				t.Fatalf("draft navigation consumed parent input: %q %v", next, err)
			}
		})
	}
}

func TestRuleDraftShowsDraftActionsOnlyWithChanges(t *testing.T) {
	env := testenv.New(t)
	version := env.RuleConfig(t, nil)
	output := testenv.CaptureOutput(t, func() {
		view := testenv.Input(t, "3\n1\n1\n1\n8443\nr\nq\n")
		if _, err := env.AccessPolicy().EditPolicyDraft(view, string(version.Content), false, nil); !errors.Is(err, ui.ErrReturnToMenu) {
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

func TestSavedRulePagesUseCurrentConfigurationLabels(t *testing.T) {
	env := testenv.New(t)
	env.RuleConfig(t, testenv.OrderRules())
	env.AcceptRuleCommands(t)
	view := testenv.Input(t, "3\n1\n1\n1\n8443\ns\n\n5\no\nq\nq\np\n\nq\n")
	output := testenv.CaptureOutput(t, func() {
		if err := env.AccessPolicy().Run(view); !errors.Is(err, ui.ErrReturnToMenu) {
			t.Fatal(err)
		}
	})
	_, saved, found := strings.Cut(output, "访问规则已保存并加载")
	if !found || !strings.Contains(saved, "访问规则（当前配置）") {
		t.Fatal("saving did not return to the current configuration")
	}
	for _, label := range []string{"草稿", "有未保存的修改"} {
		if strings.Contains(saved, label) {
			t.Fatalf("saved rule pages still show %q", label)
		}
	}
}
