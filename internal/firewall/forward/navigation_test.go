package forward_test

import (
	"errors"
	"strings"
	"testing"

	"snail_tool/internal/firewall/internal/testenv"
	"snail_tool/internal/storage"
	"snail_tool/internal/ui"
)

func TestDockerSettingsOpenFromForwardRuleManager(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	env := testenv.New(t)
	version := env.RuleConfig(t, nil)
	view := testenv.Input(t, "6\nq\n7\nq\nq\nparent-next\n")
	output := testenv.CaptureOutput(t, func() {
		if err := env.ForwardPolicy().Run(view); !errors.Is(err, ui.ErrReturnToMenu) {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "接受或拒绝\n\n  6   Docker 流量放行") ||
		!strings.Contains(output, "分别设置 Docker 网桥入站和出站\n  7   Docker 放行标记") {
		t.Fatal("Docker 菜单项应与上方规则配置隔一行并保持相邻")
	}
	for _, name := range []string{"Docker 流量放行", "Docker 放行标记"} {
		if !strings.Contains(output, "转发规则管理  ›  "+name) {
			t.Fatalf("%s did not open under the forward rule manager:\n%s", name, output)
		}
	}
	if next, err := view.ReadLine(); err != nil || next != "parent-next" {
		t.Fatalf("Docker menu navigation consumed parent input: %q %v", next, err)
	}
	if storage.ReadFileString(env.ConfigPath) != string(version.Content) {
		t.Fatal("opening Docker settings changed the firewall configuration")
	}
}
