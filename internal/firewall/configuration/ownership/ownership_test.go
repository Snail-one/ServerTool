package ownership_test

import (
	"os"
	"strings"
	"testing"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/firewall/configuration/generate"
	"snail_tool/internal/firewall/configuration/ownership"
	rulespkg "snail_tool/internal/firewall/internal/rules"
	"snail_tool/internal/firewall/settings/sshaccess"
)

func TestToolOwnershipDistinguishesOwnedPartialUnmarkedAndDamagedConfig(t *testing.T) {
	managed, _ := generate.Build([]int{2222}, false, false)
	for _, scenario := range []struct {
		name, content, want string
		fileErr             error
	}{
		{"managed", managed, "已接管", nil},
		{"unmarked", "table inet snail {}\n", "未接管", nil},
		{"missing", "", "未接管（配置不存在）", os.ErrNotExist},
		{"unreadable", "", "无法判断", os.ErrPermission},
		{"foreign config outside", managed + "\ntable inet other {}\n", "部分接管", nil},
		{"comments outside", managed + "\n# a note\n", "已接管", nil},
		{"rules block only", rulespkg.AccessBegin + "\n" + rulespkg.AccessEnd, "部分接管", nil},
		{"SSH block only", sshaccess.LegacyBegin + "\n" + sshaccess.LegacyEnd, "部分接管", nil},
		{"missing end", strings.Replace(managed, configuration.ManagedEnd, "", 1), "标记异常", nil},
		{"repeated begin", managed + configuration.ManagedBegin, "标记异常", nil},
		{"reversed", configuration.ManagedEnd + "\n" + configuration.ManagedBegin, "标记异常", nil},
		{"damaged nested block", strings.Replace(managed, rulespkg.AccessEnd, "", 1), "标记异常", nil},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			field := ownership.Field([]byte(scenario.content), scenario.fileErr)
			if field.Value != scenario.want {
				t.Fatalf("ownership=%+v want=%s", field, scenario.want)
			}
		})
	}
}
