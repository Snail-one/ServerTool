package firewall

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type portInterval struct{ first, last int }
type portIntervals []portInterval

func allPorts() portIntervals { return portIntervals{{1, 65535}} }

func unionPorts(values portIntervals) portIntervals {
	values = append(portIntervals(nil), values...)
	sort.Slice(values, func(i, j int) bool { return values[i].first < values[j].first })
	var result portIntervals
	for _, value := range values {
		if len(result) == 0 || value.first > result[len(result)-1].last+1 {
			result = append(result, value)
		} else if value.last > result[len(result)-1].last {
			result[len(result)-1].last = value.last
		}
	}
	return result
}

func intersectPorts(a, b portIntervals) portIntervals {
	var result portIntervals
	for _, left := range a {
		for _, right := range b {
			first, last := max(left.first, right.first), min(left.last, right.last)
			if first <= last {
				result = append(result, portInterval{first, last})
			}
		}
	}
	return unionPorts(result)
}

func subtractPorts(values, removed portIntervals) portIntervals {
	result := append(portIntervals(nil), values...)
	for _, cut := range removed {
		var next portIntervals
		for _, value := range result {
			if cut.last < value.first || cut.first > value.last {
				next = append(next, value)
				continue
			}
			if value.first < cut.first {
				next = append(next, portInterval{value.first, cut.first - 1})
			}
			if value.last > cut.last {
				next = append(next, portInterval{cut.last + 1, value.last})
			}
		}
		result = next
	}
	return result
}

func (values portIntervals) String() string {
	values = unionPorts(values)
	if len(values) == 0 {
		return "无业务端口放行"
	}
	if len(values) == 1 && values[0] == (portInterval{1, 65535}) {
		return "全部端口"
	}
	var parts []string
	for _, value := range values {
		part := strconv.Itoa(value.first)
		if value.first != value.last {
			part += "-" + strconv.Itoa(value.last)
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ", ")
}

func nftInt(value any) (int, bool) {
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

type livePortParser struct{ sets map[string]any }

func (parser livePortParser) values(value any, depth int) ([]any, error) {
	if depth > 12 {
		return nil, fmt.Errorf("集合引用嵌套过深")
	}
	if name, ok := value.(string); ok && strings.HasPrefix(name, "@") {
		value, exists := parser.sets[strings.TrimPrefix(name, "@")]
		if !exists {
			return nil, fmt.Errorf("未读取到集合 %s", name)
		}
		return parser.values(value, depth+1)
	}
	if object, ok := value.(map[string]any); ok {
		if set, exists := object["set"]; exists {
			return parser.values(set, depth+1)
		}
		if elem, ok := object["elem"].(map[string]any); ok {
			return parser.values(elem["val"], depth+1)
		}
	}
	if list, ok := value.([]any); ok {
		var result []any
		for _, item := range list {
			children, err := parser.values(item, depth+1)
			if err != nil {
				return nil, err
			}
			result = append(result, children...)
		}
		return result, nil
	}
	return []any{value}, nil
}

func (parser livePortParser) ports(value any) (portIntervals, error) {
	values, err := parser.values(value, 0)
	if err != nil {
		return nil, err
	}
	var result portIntervals
	for _, value := range values {
		first, last := 0, 0
		if object, ok := value.(map[string]any); ok {
			bounds, ok := object["range"].([]any)
			if !ok || len(bounds) != 2 {
				return nil, fmt.Errorf("不支持的端口表达式")
			}
			var firstOK, lastOK bool
			first, firstOK = nftInt(bounds[0])
			last, lastOK = nftInt(bounds[1])
			if !firstOK || !lastOK {
				return nil, fmt.Errorf("端口范围不是数字")
			}
		} else {
			var ok bool
			first, ok = nftInt(value)
			if !ok {
				return nil, fmt.Errorf("端口不是数字")
			}
			last = first
		}
		if first < 1 || last > 65535 || last < first {
			return nil, fmt.Errorf("端口范围超出 1-65535")
		}
		result = append(result, portInterval{first, last})
	}
	return unionPorts(result), nil
}
