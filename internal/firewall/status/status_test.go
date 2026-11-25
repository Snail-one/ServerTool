package status_test

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/firewall/configuration/generate"
	"snail_tool/internal/firewall/internal/testenv"
	"snail_tool/internal/firewall/loadedrules"
	autostartconfig "snail_tool/internal/firewall/settings/autostart"
	"snail_tool/internal/firewall/settings/counters"
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
			env := testenv.New(t)
			output := fmt.Sprintf(`{"nftables":[%s]}`, scenario.objects)
			live := loadedrules.Parse(output)
			if live.Err != nil || live.InputActive != scenario.active {
				t.Fatalf("incorrect input-hook state: %+v", live)
			}
			env.CommandOutput = func(name string, args ...string) (string, error) {
				if name != "nft" || !reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
					t.Fatalf("unexpected query: %s %v", name, args)
				}
				return output, nil
			}
			// Boot enablement must never stand in for current kernel state.
			for _, boot := range []string{"已开启", "已关闭"} {
				fields := env.StatusFields(autostartconfig.Status{Label: boot})
				if fields[0].Label != "防火墙" || testenv.FieldValue(fields, "防火墙") != scenario.state || testenv.FieldValue(fields, "开机自启") != boot {
					t.Fatalf("runtime and startup state conflated: %+v", fields)
				}
				if scenario.active && testenv.FieldValue(fields, "入站策略") == "" {
					t.Fatal("active input policy missing")
				}
				if scenario.name == "other firewall" && testenv.FieldValue(fields, "其他入站链") == "" {
					t.Fatal("must acknowledge other inbound filters when this tool is disabled")
				}
			}
		})
	}
}

func TestStatusCountsOwnRulesAndKeepsDetectionFailureUnknown(t *testing.T) {
	env := testenv.New(t)
	env.CommandOutput = func(string, ...string) (string, error) { return testenv.PortRules(), nil }
	fields := env.StatusFields(autostartconfig.Status{Label: "已关闭"})
	if testenv.FieldValue(fields, "本工具规则") != "4 条链 · 26 条规则" {
		t.Fatalf("counts must be scoped to inet snail: %s", testenv.FieldValue(fields, "本工具规则"))
	}
	env.CommandExists = func(string) bool { return false }
	fields = env.StatusFields(autostartconfig.Status{Label: "不支持"})
	if testenv.FieldValue(fields, "防火墙") != "无法检测（未安装 nftables）" {
		t.Fatalf("missing nft must not imply all firewalls are disabled: %+v", fields)
	}
}

func TestPacketCounterStatusUsesLoadedRulesAndOneSharedQuery(t *testing.T) {
	counted := `{"counter":{"packets":0,"bytes":0}},{"accept":null}`
	unCounted := `{"drop":null}`
	rule := func(expr string) string {
		return `{"rule":{"family":"inet","table":"snail","chain":"input","expr":[` + expr + `]}}`
	}
	branch := rule(`{"jump":{"target":"public_ports"}}`) + "," + rule(`{"return":null}`)
	foreign := `{"rule":{"family":"ip","table":"snail","chain":"input","expr":[{"counter":null},{"accept":null}]}}`
	for _, scenario := range []struct {
		name, rules, want string
		installed         bool
		queryErr          error
	}{
		{"zero counts still enabled", rule(counted) + "," + branch, ui.PrimaryText("已开启"), true, nil},
		{"populated counts", rule(strings.Replace(counted, `"packets":0`, `"packets":100`, 1)), ui.PrimaryText("已开启"), true, nil},
		{"stateless listing", rule(`{"counter":null},{"drop":null}`), ui.PrimaryText("已开启"), true, nil},
		{"named counter", rule(`{"counter":"manual"},{"reject":null}`), ui.PrimaryText("已开启"), true, nil},
		{"no counters", rule(unCounted) + "," + branch, "已关闭", true, nil},
		{"mixed", rule(counted) + "," + rule(unCounted), "部分开启", true, nil},
		{"other table ignored", rule(unCounted) + "," + foreign, "已关闭", true, nil},
		{"no rule counters", branch, "已关闭", true, nil},
		{"missing expressions", `{"rule":{"family":"inet","table":"snail","chain":"input"}}`, "无法判断", true, nil},
		{"not installed", "", "无法检测", false, nil},
		{"query failed", "", "检测失败", true, errors.New("permission denied")},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			env := testenv.New(t)
			// Keep the saved setting opposite to the loaded state where possible.
			// A pending edit must not be presented as the kernel's current switch.
			base, _ := generate.Build([]int{2222}, false, false)
			content, err := testenv.WithPacketCounters(base, scenario.want != ui.PrimaryText("已开启"))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(env.ConfigPath, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			env.CommandExists = func(name string) bool { return name == "nft" && scenario.installed }
			queries := 0
			env.CommandOutput = func(name string, args ...string) (string, error) {
				if name != "nft" || !reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
					t.Fatalf("unexpected query: %s %v", name, args)
				}
				queries++
				objects := `{"table":{"family":"inet","name":"snail"}},` +
					`{"chain":{"family":"inet","table":"snail","name":"input","type":"filter","hook":"input","policy":"drop"}}`
				if scenario.rules != "" {
					objects += "," + scenario.rules
				}
				return `{"nftables":[` + objects + `]}`, scenario.queryErr
			}
			fields := env.StatusFields(autostartconfig.Status{Label: "已关闭"})
			if got := testenv.FieldValue(fields, "数据包统计"); got != scenario.want {
				t.Fatalf("counter status=%q, want %q", got, scenario.want)
			}
			wantQueries := 0
			if scenario.installed {
				wantQueries = 1
			}
			if queries != wantQueries {
				t.Fatalf("status must share the existing ruleset query: %d", queries)
			}
		})
	}
	for _, output := range []string{
		`{"nftables":[]}`,
		`{"nftables":[{"table":{"family":"inet","name":"snail","flags":["dormant"]}}]}`,
	} {
		live := loadedrules.Parse(output)
		if got := counters.StateField(true, live.InputActive, live.JSON, live.Err); got.Value != "未启用本工具过滤" {
			t.Fatalf("inactive firewall misreported as a counter switch: %+v", got)
		}
	}
}

func TestCurrentConfigFieldsShowsOnlyFirewallStatus(t *testing.T) {
	for _, scenario := range []string{"managed", "unmarked", "empty", "missing", "query failure"} {
		t.Run(scenario, func(t *testing.T) {
			env := testenv.New(t)
			content, _ := generate.Build([]int{2222}, false, false)
			if scenario == "unmarked" {
				content = "table inet filter {}\n"
			} else if scenario == "empty" {
				content = " \n"
			}
			if scenario != "missing" {
				if err := os.WriteFile(env.ConfigPath, []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
			}
			env.CommandOutput = func(name string, args ...string) (string, error) {
				if name != "nft" || !reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
					t.Fatalf("unexpected status query: %s %v", name, args)
				}
				if scenario == "query failure" {
					return "permission denied", errors.New("exit status 1")
				}
				return `{"nftables":[{"metainfo":{"version":"1.1.3"}}]}`, nil
			}
			fields := env.StatusFields(env.Autostart().Inspect())
			for _, label := range []string{"配置文件", "文件状态", "最后修改", "本工具表", "生成配置", "手动编辑"} {
				if testenv.FieldValue(fields, label) != "" {
					t.Fatalf("status card must not include %s", label)
				}
			}
			if scenario == "query failure" {
				if testenv.FieldValue(fields, "防火墙") != "检测失败" || testenv.FieldValue(fields, "入站策略") != "" {
					t.Fatal("query failure must not be reported as unloaded")
				}
			} else if testenv.FieldValue(fields, "防火墙") != "未开启（本工具）" || !strings.Contains(testenv.FieldValue(fields, "nftables"), "v1.1.3") {
				t.Fatal("saved config must not imply loaded rules; installed version should be shown")
			}
			if scenario != "query failure" && len(fields) != 9 {
				t.Fatalf("unloaded status card should have nine fields, got %d", len(fields))
			}
			if scenario == "managed" && testenv.FieldValue(fields, "工具接管") != "已接管" {
				t.Fatal("owned config should show managed even when not active")
			}
		})
	}
}
