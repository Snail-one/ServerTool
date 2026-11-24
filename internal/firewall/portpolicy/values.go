package portpolicy

import (
	"fmt"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/firewall/internal/nftjson"
)

type livePortParser struct{ sets map[string]any }

func (parser livePortParser) ports(value any) (configuration.PortIntervals, error) {
	values, err := nftjson.Values(value, parser.sets, 0)
	if err != nil {
		return nil, err
	}
	var result configuration.PortIntervals
	for _, value := range values {
		first, last := 0, 0
		if object, ok := value.(map[string]any); ok {
			bounds, ok := object["range"].([]any)
			if !ok || len(bounds) != 2 {
				return nil, fmt.Errorf("不支持的端口表达式")
			}
			var firstOK, lastOK bool
			first, firstOK = nftjson.Int(bounds[0])
			last, lastOK = nftjson.Int(bounds[1])
			if !firstOK || !lastOK {
				return nil, fmt.Errorf("端口范围不是数字")
			}
		} else {
			var ok bool
			first, ok = nftjson.Int(value)
			if !ok {
				return nil, fmt.Errorf("端口不是数字")
			}
			last = first
		}
		if first < 1 || last > 65535 || last < first {
			return nil, fmt.Errorf("端口范围超出 1-65535")
		}
		result = append(result, configuration.PortInterval{First: first, Last: last})
	}
	return configuration.UnionPorts(result), nil
}
