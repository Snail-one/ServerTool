package configuration

import (
	"sort"
	"strconv"
	"strings"
)

type PortInterval struct{ First, Last int }
type PortIntervals []PortInterval

func AllPorts() PortIntervals { return PortIntervals{{1, 65535}} }

func UnionPorts(values PortIntervals) PortIntervals {
	values = append(PortIntervals(nil), values...)
	sort.Slice(values, func(i, j int) bool { return values[i].First < values[j].First })
	var result PortIntervals
	for _, value := range values {
		if len(result) == 0 || value.First > result[len(result)-1].Last+1 {
			result = append(result, value)
		} else if value.Last > result[len(result)-1].Last {
			result[len(result)-1].Last = value.Last
		}
	}
	return result
}

func IntersectPorts(a, b PortIntervals) PortIntervals {
	var result PortIntervals
	for _, left := range a {
		for _, right := range b {
			first, last := max(left.First, right.First), min(left.Last, right.Last)
			if first <= last {
				result = append(result, PortInterval{first, last})
			}
		}
	}
	return UnionPorts(result)
}

func SubtractPorts(values, removed PortIntervals) PortIntervals {
	result := append(PortIntervals(nil), values...)
	for _, cut := range removed {
		var next PortIntervals
		for _, value := range result {
			if cut.Last < value.First || cut.First > value.Last {
				next = append(next, value)
				continue
			}
			if value.First < cut.First {
				next = append(next, PortInterval{value.First, cut.First - 1})
			}
			if value.Last > cut.Last {
				next = append(next, PortInterval{cut.Last + 1, value.Last})
			}
		}
		result = next
	}
	return result
}

func (values PortIntervals) String() string {
	values = UnionPorts(values)
	if len(values) == 0 {
		return "无业务端口放行"
	}
	if len(values) == 1 && values[0] == (PortInterval{1, 65535}) {
		return "全部端口"
	}
	var parts []string
	for _, value := range values {
		part := strconv.Itoa(value.First)
		if value.First != value.Last {
			part += "-" + strconv.Itoa(value.Last)
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ", ")
}
