package configuration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"snail_tool/internal/firewall/internal/nftjson"
	"snail_tool/internal/firewall/loadedrules"
	"snail_tool/internal/ui"
)

// The first nft process must run only after unshare creates a fresh network
// namespace. A failed unshare must never fall back to the host namespace.
const RenderConfigScript = `nft -f "$1" >/dev/null && nft -s -j list ruleset`

func (store Store) SyncField(content []byte, fileErr error, installed bool, liveJSON string, liveErr error, commandExists func(string) bool) ui.CardField {
	field := ui.CardField{Label: "配置同步"}
	switch {
	case fileErr != nil:
		field.Value, field.Detail = "无法比较", "配置文件不存在或无法读取"
		return field
	case len(bytes.TrimSpace(content)) == 0:
		field.Value, field.Detail = "无法比较", "配置文件为空"
		return field
	case !installed || liveErr != nil:
		field.Value, field.Detail = "检测失败", "无法读取当前内核规则"
		return field
	}
	equal, err := store.Compare(content, liveJSON, liveErr, commandExists)
	if err != nil {
		field.Value, field.Detail = "无法比较", err.Error()
		return field
	}
	field.Detail = "对比文件涉及的表，忽略计数器数值和句柄"
	if equal {
		field.Value = "已同步 · 当前文件与已加载规则一致"
	} else {
		field.Value = "未同步 · 配置与内核规则不同"
		field.Detail += "；输入 r 校验加载配置"
	}
	return field
}

func (store Store) compareLoaded(content []byte, commandExists func(string) bool) (bool, error) {
	if !commandExists("unshare") {
		return false, fmt.Errorf("缺少 unshare，无法对比配置与内核规则")
	}
	live := loadedrules.Inspect(store.CommandOutput)
	return store.Compare(content, live.JSON, live.Err, commandExists)
}

func (store Store) Compare(content []byte, liveJSON string, liveErr error, commandExists func(string) bool) (bool, error) {
	if liveErr != nil {
		return false, fmt.Errorf("无法读取当前内核规则: %w", liveErr)
	}
	if !commandExists("unshare") {
		return false, fmt.Errorf("缺少 unshare，请安装 util-linux")
	}
	expected, err := store.CommandOutput("unshare", "--net", "--", "sh", "-c", RenderConfigScript, "snail-nft-compare", store.Path)
	if err != nil {
		return false, fmt.Errorf("隔离解析配置失败: %w: %s", err, strings.Join(strings.Fields(expected), " "))
	}
	// A comparison is valid only for the same file snapshot used by the caller.
	latest, err := os.ReadFile(store.Path)
	if err != nil || !bytes.Equal(content, latest) {
		return false, fmt.Errorf("比较期间配置文件已变化，请刷新后重试")
	}
	return EquivalentRules(expected, liveJSON)
}

// EquivalentRules compares each table produced by the configuration. Other
// tables (for example those maintained by Docker) are outside this scope.
func EquivalentRules(expected, actual string) (bool, error) {
	wanted, err := nftjson.Decode(expected)
	if err != nil {
		return false, err
	}
	loaded, err := nftjson.Decode(actual)
	if err != nil {
		return false, err
	}
	tables := make(map[string]bool)
	for _, object := range wanted {
		if table, ok := object["table"].(map[string]any); ok {
			tables[tableKey(table["family"], table["name"])] = true
		}
	}
	if len(tables) == 0 {
		return false, fmt.Errorf("配置文件未生成可比较的表")
	}
	want, err := canonicalRules(wanted, tables)
	if err != nil {
		return false, err
	}
	got, err := canonicalRules(loaded, tables)
	if err != nil {
		return false, err
	}
	return bytes.Equal(want, got), nil
}

func tableKey(family, name any) string {
	return fmt.Sprintf("%v/%v", family, name)
}

func canonicalRules(objects []map[string]any, tables map[string]bool) ([]byte, error) {
	// Group rules by chain so their order is retained, while the listing order
	// of independent tables, chains and named objects does not affect equality.
	groups := make(map[string][]any)
	for _, object := range objects {
		for kind, raw := range object {
			if kind == "metainfo" {
				continue
			}
			body, ok := raw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("无法比较规则对象: %s", kind)
			}
			table := body["table"]
			if kind == "table" {
				table = body["name"]
			}
			key := tableKey(body["family"], table)
			if !tables[key] {
				continue
			}
			delete(body, "handle")
			delete(body, "index")
			delete(body, "position")
			id := body["name"]
			if kind == "rule" {
				id = body["chain"]
			}
			group := fmt.Sprintf("%s/%s/%v", key, kind, id)
			groups[group] = append(groups[group], normalizeRuleValue(body, kind))
		}
	}
	return json.Marshal(groups)
}

func normalizeRuleValue(value any, context string) any {
	switch item := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(item))
		for key, child := range item {
			if context == "counter" && (key == "packets" || key == "bytes") {
				continue
			}
			if context == "quota" && key == "used" {
				continue
			}
			result[key] = normalizeRuleValue(child, key)
		}
		if context == "counter" && len(result) == 0 {
			return nil
		}
		return result
	case []any:
		result := make([]any, len(item))
		for i, child := range item {
			result[i] = normalizeRuleValue(child, context)
		}
		// Set contents and flag lists are unordered; expression and rule arrays
		// remain ordered because their sequence changes firewall behaviour.
		if context == "set" || context == "elem" || context == "flags" || context == "devices" {
			sort.SliceStable(result, func(i, j int) bool {
				a, _ := json.Marshal(result[i])
				b, _ := json.Marshal(result[j])
				return bytes.Compare(a, b) < 0
			})
		}
		return result
	default:
		return value
	}
}
