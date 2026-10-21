package firewall

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"snail_tool/internal/ui"
)

// The first nft process must run only after unshare creates a fresh network
// namespace. A failed unshare must never fall back to the host namespace.
const renderConfigScript = `nft -f "$1" >/dev/null && nft -s -j list ruleset`

func configSyncField(content []byte, fileErr error, installed bool, live liveRules) ui.CardField {
	field := ui.CardField{Label: "配置同步"}
	switch {
	case fileErr != nil:
		field.Value, field.Detail = "无法比较", "配置文件不存在或无法读取"
		return field
	case len(bytes.TrimSpace(content)) == 0:
		field.Value, field.Detail = "无法比较", "配置文件为空"
		return field
	case !installed || live.err != nil:
		field.Value, field.Detail = "检测失败", "无法读取当前内核规则"
		return field
	case !commandExists("unshare"):
		field.Value, field.Detail = "无法比较", "缺少 unshare，请安装 util-linux"
		return field
	}
	expected, err := commandOutput("unshare", "--net", "--", "sh", "-c", renderConfigScript, "snail-nft-compare", configPath)
	if err != nil {
		field.Value = "无法比较"
		field.Detail = fmt.Sprintf("隔离解析配置失败: %v: %s", err, strings.Join(strings.Fields(expected), " "))
		return field
	}
	// A concurrent edit invalidates the comparison just as it invalidates a
	// load confirmation. Do not attach a synchronized state to a different file.
	latest, err := os.ReadFile(configPath)
	if err != nil || !bytes.Equal(content, latest) {
		field.Value, field.Detail = "无法比较", "比较期间配置文件已变化，请刷新后重试"
		return field
	}
	equal, err := equivalentRules(expected, live.json)
	if err != nil {
		field.Value, field.Detail = "无法比较", err.Error()
		return field
	}
	field.Detail = "对比文件涉及的表，忽略计数器数值和句柄"
	if equal {
		field.Value = "已同步 · 当前文件与已加载规则一致"
	} else {
		field.Value = "未同步 · 配置与内核规则不同"
		field.Detail += "；选择 5 reload"
	}
	return field
}

// equivalentRules compares each table produced by the configuration. Other
// tables (for example those maintained by Docker) are outside this scope.
func equivalentRules(expected, actual string) (bool, error) {
	wanted, err := decodeRules(expected)
	if err != nil {
		return false, err
	}
	loaded, err := decodeRules(actual)
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

func decodeRules(content string) ([]map[string]any, error) {
	var result struct {
		Objects []map[string]any `json:"nftables"`
	}
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("解析规则集失败: %w", err)
	}
	if result.Objects == nil {
		return nil, fmt.Errorf("规则集缺少 nftables 数据")
	}
	return result.Objects, nil
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
