package nftjson

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func Int(value any) (int, bool) {
	var raw string
	switch value := value.(type) {
	case json.Number:
		raw = value.String()
	case string:
		raw = value
	default:
		return 0, false
	}
	result, err := strconv.Atoi(raw)
	return result, err == nil
}

func Values(value any, sets map[string]any, depth int) ([]any, error) {
	if depth > 12 {
		return nil, fmt.Errorf("集合引用嵌套过深")
	}
	if name, ok := value.(string); ok && strings.HasPrefix(name, "@") {
		value, exists := sets[strings.TrimPrefix(name, "@")]
		if !exists {
			return nil, fmt.Errorf("未读取到集合 %s", name)
		}
		return Values(value, sets, depth+1)
	}
	if object, ok := value.(map[string]any); ok {
		if set, exists := object["set"]; exists {
			return Values(set, sets, depth+1)
		}
		if elem, ok := object["elem"].(map[string]any); ok {
			return Values(elem["val"], sets, depth+1)
		}
	}
	if list, ok := value.([]any); ok {
		var result []any
		for _, item := range list {
			children, err := Values(item, sets, depth+1)
			if err != nil {
				return nil, err
			}
			result = append(result, children...)
		}
		return result, nil
	}
	return []any{value}, nil
}
