package cleanup

import (
	"fmt"
	"strconv"
	"strings"

	"snail_tool/internal/container/runtime"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

func printContainerDiskUsage(rt runtime.Runtime, title string) {
	fields, err := containerDiskUsageFields(rt, system.Output)
	if err != nil {
		fields = []ui.CardField{{Label: "状态", Value: "无法获取磁盘占用", Detail: err.Error()}}
	} else if len(fields) == 0 {
		fields = []ui.CardField{{Label: "状态", Value: "暂无磁盘占用数据"}}
	}
	ui.PrintSectionCard(title, fields...)
}

func containerDiskUsageFields(rt runtime.Runtime, output func(string, ...string) (string, error)) ([]ui.CardField, error) {
	total := "{{.TotalCount}}"
	if rt.Name == "podman" {
		total = "{{.Total}}"
	}
	// Explicit separators avoid parsing the CLI's table padding or splitting
	// resource names and sizes that contain spaces.
	format := "{{.Type}}\t" + total + "\t{{.Active}}\t{{.Size}}\t{{.Reclaimable}}"
	text, err := output(rt.Name, "system", "df", "--format", format)
	if err != nil {
		detail := strings.Join(strings.Fields(text), " ")
		if detail == "" {
			return nil, fmt.Errorf("%s system df 失败: %w", rt.Display, err)
		}
		return nil, fmt.Errorf("%s system df 失败: %w；%s", rt.Display, err, detail)
	}
	var fields []ui.CardField
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		columns := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(columns) != 5 {
			return nil, fmt.Errorf("%s 磁盘占用输出格式无法识别", rt.Display)
		}
		for i := range columns {
			columns[i] = strings.TrimSpace(columns[i])
		}
		for _, count := range columns[1:3] {
			if _, err := strconv.ParseUint(count, 10, 64); err != nil {
				return nil, fmt.Errorf("%s 磁盘占用数量无法识别", rt.Display)
			}
		}
		if columns[0] == "" || columns[3] == "" || columns[4] == "" {
			return nil, fmt.Errorf("%s 磁盘占用输出不完整", rt.Display)
		}
		label := columns[0]
		switch label {
		case "Images":
			label = "镜像"
		case "Containers":
			label = "容器"
		case "Local Volumes":
			label = "数据卷"
		case "Build Cache":
			label = "构建缓存"
		}
		fields = append(fields, ui.CardField{
			Label:  label,
			Value:  "占用 " + columns[3] + " · 可回收 " + columns[4],
			Detail: "总数 " + columns[1] + " · 活跃 " + columns[2],
		})
	}
	return fields, nil
}
