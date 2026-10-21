package firewall

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/ui"
)

func TestParseLiveRulesCountsAllTablesAndScopesPolicyToSnail(t *testing.T) {
	output := `{"nftables":[
		{"metainfo":{"version":"1.1.3"}},
		{"table":{"family":"inet","name":"snail"}},
		{"table":{"family":"ip","name":"docker"}},
		{"table":{"family":"ip6","name":"other"}},
		{"chain":{"family":"inet","table":"snail","name":"input","hook":"input","policy":"drop"}},
		{"chain":{"family":"ip","table":"docker","name":"forward","hook":"forward","policy":"accept"}},
		{"chain":{"family":"ip6","table":"other","name":"input","hook":"input","policy":"accept"}},
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

func TestCurrentConfigFieldsSeparatesSavedFileAndLiveStatus(t *testing.T) {
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
			fields := currentConfigFields()
			state := fieldValue(fields, "文件状态")
			want := map[string]string{
				"managed": "含本工具标记", "unmarked": "来源未标记", "empty": "空文件", "missing": "尚未生成", "query failure": "含本工具标记",
			}[scenario]
			if !strings.Contains(state, want) {
				t.Fatalf("file state=%q, want %q", state, want)
			}
			if scenario == "query failure" {
				if fieldValue(fields, "内核规则") != "检测失败" || fieldValue(fields, "本工具表") != "" {
					t.Fatal("query failure must not be reported as unloaded")
				}
			} else if fieldValue(fields, "本工具表") != "未加载 inet snail 表" || !strings.Contains(fieldValue(fields, "nftables"), "v1.1.3") {
				t.Fatal("saved config must not imply loaded rules; installed version should be shown")
			}
			if fieldValue(fields, "生成配置") != "校验、备份并写入后自动加载" || !strings.Contains(fieldValue(fields, "手动编辑"), "选择 5") {
				t.Fatal("generation and manual editing must have separate activation descriptions")
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
