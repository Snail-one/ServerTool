package nftjson

import (
	"encoding/json"
	"fmt"
	"strings"
)

func Decode(content string) ([]map[string]any, error) {
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
