package ownership

import (
	"os"
	"strings"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/firewall/forward/dockerbridge"
	"snail_tool/internal/firewall/internal/rules"
	"snail_tool/internal/firewall/settings/ping"
	"snail_tool/internal/firewall/settings/sshaccess"
	"snail_tool/internal/ui"
)

var managedMarkers = [...][2]string{
	{configuration.ManagedBegin, configuration.ManagedEnd},
	{rules.AccessBegin, rules.AccessEnd},
	{sshaccess.LegacyBegin, sshaccess.LegacyEnd},
	{ping.Begin, ping.End},
	{dockerbridge.Begin, dockerbridge.End},
}

func HasManagedConfig(content string) bool {
	for _, markers := range managedMarkers {
		if strings.Contains(content, markers[0]) || strings.Contains(content, markers[1]) {
			return true
		}
	}
	return false
}

// Field reports configuration metadata, independent of kernel activation and
// synchronization. A matching table name alone does not establish ownership.
func Field(data []byte, fileErr error) ui.CardField {
	field := ui.CardField{Label: "工具接管", Value: "未接管"}
	if os.IsNotExist(fileErr) {
		field.Value = "未接管（配置不存在）"
		return field
	}
	if fileErr != nil {
		field.Value, field.Detail = "无法判断", fileErr.Error()
		return field
	}
	content := string(data)
	mainStart, mainEnd := -1, -1
	for _, markers := range managedMarkers {
		begins, ends := strings.Count(content, markers[0]), strings.Count(content, markers[1])
		if begins == 0 && ends == 0 {
			continue
		}
		begin, end := strings.Index(content, markers[0]), strings.Index(content, markers[1])
		if begins != 1 || ends != 1 || end < begin {
			field.Value = "标记异常"
			return field
		}
		field.Value = "部分接管"
		if markers[0] == configuration.ManagedBegin {
			mainStart, mainEnd = begin, end+len(configuration.ManagedEnd)
		}
	}
	if mainStart >= 0 {
		outside, err := configuration.Tokens(content[:mainStart] + content[mainEnd:])
		if err == nil && len(outside) == 0 {
			field.Value = "已接管"
		}
	}
	return field
}
