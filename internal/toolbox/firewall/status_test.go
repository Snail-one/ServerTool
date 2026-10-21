package firewall

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/ui"
)

func TestFirewallEnabledStateRequiresLiveInputHook(t *testing.T) {
	table := `{"table":{"family":"inet","name":"snail"}}`
	chain := `{"chain":{"family":"inet","table":"snail","name":"input","type":"filter","hook":"input","policy":"drop"}}`
	foreign := `{"table":{"family":"inet","name":"other"}},{"chain":{"family":"inet","table":"other","name":"input","type":"filter","hook":"input","policy":"drop"}}`
	for _, scenario := range []struct {
		name, objects, state string
		active               bool
	}{
		{"empty", "", "未开启（本工具）", false},
		{"table only", table, "未开启（入站链未挂接）", false},
		{"regular chain", table + "," + strings.Replace(chain, `,"hook":"input"`, "", 1), "未开启（入站链未挂接）", false},
		{"forward hook", table + "," + strings.Replace(chain, `"hook":"input"`, `"hook":"forward"`, 1), "未开启（入站链未挂接）", false},
		{"nat chain", table + "," + strings.Replace(chain, `"type":"filter"`, `"type":"nat"`, 1), "未开启（入站链未挂接）", false},
		{"active", table + "," + chain, "已开启（本工具入站规则）", true},
		{"accept policy", table + "," + strings.Replace(chain, `"policy":"drop"`, `"policy":"accept"`, 1), "已开启（本工具入站规则）", true},
		{"custom chain name", table + "," + strings.Replace(chain, `"name":"input"`, `"name":"custom_input"`, 1), "已开启（本工具入站规则）", true},
		{"table listed last", chain + "," + table, "已开启（本工具入站规则）", true},
		{"dormant table", chain + `,{"table":{"family":"inet","name":"snail","flags":["dormant"]}}`, "未开启（本工具表已停用）", false},
		{"other firewall", foreign, "未开启（本工具）", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			setupTest(t)
			output := fmt.Sprintf(`{"nftables":[%s]}`, scenario.objects)
			live := parseLiveRules(output)
			if live.err != nil || live.inputActive != scenario.active {
				t.Fatalf("incorrect input-hook state: %+v", live)
			}
			commandOutput = func(name string, args ...string) (string, error) {
				if name != "nft" || !reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
					t.Fatalf("unexpected query: %s %v", name, args)
				}
				return output, nil
			}
			// Boot enablement must never stand in for current kernel state.
			for _, boot := range []string{"已开启", "已关闭"} {
				fields := currentConfigFields(autostartStatus{label: boot})
				if fields[0].Label != "防火墙" || fieldValue(fields, "防火墙") != scenario.state || fieldValue(fields, "开机自启") != boot {
					t.Fatalf("runtime and startup state conflated: %+v", fields)
				}
				if scenario.active && fieldValue(fields, "入站策略") == "" {
					t.Fatal("active input policy missing")
				}
				if scenario.name == "other firewall" && fieldValue(fields, "其他入站链") == "" {
					t.Fatal("must acknowledge other inbound filters when this tool is disabled")
				}
			}
		})
	}
}

func TestStatusCountsOwnRulesAndKeepsDetectionFailureUnknown(t *testing.T) {
	setupTest(t)
	commandOutput = func(string, ...string) (string, error) { return portFixture(t), nil }
	fields := currentConfigFields(autostartStatus{label: "已关闭"})
	if fieldValue(fields, "本工具规则") != "4 条链 · 26 条规则" {
		t.Fatalf("counts must be scoped to inet snail: %s", fieldValue(fields, "本工具规则"))
	}
	commandExists = func(string) bool { return false }
	fields = currentConfigFields(autostartStatus{label: "不支持"})
	if fieldValue(fields, "防火墙") != "无法检测（未安装 nftables）" {
		t.Fatalf("missing nft must not imply all firewalls are disabled: %+v", fields)
	}
}

func TestParseLiveRulesCountsAllTablesAndScopesPolicyToSnail(t *testing.T) {
	output := `{"nftables":[
		{"metainfo":{"version":"1.1.3"}},
		{"table":{"family":"inet","name":"snail"}},
		{"table":{"family":"ip","name":"docker"}},
		{"table":{"family":"ip6","name":"other"}},
		{"chain":{"family":"inet","table":"snail","name":"input","type":"filter","hook":"input","policy":"drop"}},
		{"chain":{"family":"ip","table":"docker","name":"forward","type":"filter","hook":"forward","policy":"accept"}},
		{"chain":{"family":"ip6","table":"other","name":"input","type":"filter","hook":"input","policy":"accept"}},
		{"rule":{"family":"inet","table":"snail","chain":"input","expr":[{"accept":null}]}},
		{"rule":{"family":"ip","table":"docker","chain":"forward","expr":[{"accept":null}]}}
	]}`
	got := parseLiveRules(output)
	if got.err != nil || got.version != "1.1.3" || got.tables != 3 || got.chains != 3 || got.rules != 2 || !got.snailLoaded {
		t.Fatalf("incorrect full ruleset summary: %+v", got)
	}
	if got.inputPolicy != "input：默认丢弃（drop）" {
		t.Fatalf("policy must come only from inet snail: %q", got.inputPolicy)
	}
	if !got.inputActive || got.snailChains != 1 || got.snailRules != 1 || got.otherInputChains != 1 {
		t.Fatalf("incorrect active state or own-table counts: %+v", got)
	}
	for _, output := range []string{`{"nftables":[]}`, `{"nftables":[{"table":{"family":"ip","name":"snail"}}]}`} {
		if got := parseLiveRules(output); got.err != nil || got.snailLoaded {
			t.Fatalf("missing inet snail table misreported: %+v", got)
		}
	}
	for _, output := range []string{`{}`, `{"nftables":null}`, `invalid json`} {
		if got := parseLiveRules(output); got.err == nil {
			t.Fatalf("unreadable status must be an error, not an empty ruleset: %q", output)
		}
	}
}

func TestCurrentConfigFieldsShowsOnlyFirewallStatus(t *testing.T) {
	for _, scenario := range []string{"managed", "unmarked", "empty", "missing", "query failure"} {
		t.Run(scenario, func(t *testing.T) {
			setupTest(t)
			content, _ := buildConfig([]int{2222}, false, false)
			if scenario == "unmarked" {
				content = "table inet filter {}\n"
			} else if scenario == "empty" {
				content = " \n"
			}
			if scenario != "missing" {
				if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
			}
			commandOutput = func(name string, args ...string) (string, error) {
				if name != "nft" || !reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
					t.Fatalf("unexpected status query: %s %v", name, args)
				}
				if scenario == "query failure" {
					return "permission denied", errors.New("exit status 1")
				}
				return `{"nftables":[{"metainfo":{"version":"1.1.3"}}]}`, nil
			}
			fields := currentConfigFields(inspectAutostart())
			for _, label := range []string{"配置文件", "文件状态", "最后修改", "本工具表", "生成配置", "手动编辑"} {
				if fieldValue(fields, label) != "" {
					t.Fatalf("status card must not include %s", label)
				}
			}
			if scenario == "query failure" {
				if fieldValue(fields, "防火墙") != "检测失败" || fieldValue(fields, "入站策略") != "" {
					t.Fatal("query failure must not be reported as unloaded")
				}
			} else if fieldValue(fields, "防火墙") != "未开启（本工具）" || !strings.Contains(fieldValue(fields, "nftables"), "v1.1.3") {
				t.Fatal("saved config must not imply loaded rules; installed version should be shown")
			}
			if scenario != "query failure" && len(fields) != 4 {
				t.Fatalf("unloaded status card should have four fields, got %d", len(fields))
			}
		})
	}
}

func fieldValue(fields []ui.CardField, label string) string {
	for _, field := range fields {
		if field.Label == label {
			return field.Value
		}
	}
	return ""
}
