package forward

import (
	"snail_tool/internal/ui"
)

const Title = "转发规则管理"
const Label = "转发规则"

func printDockerOptions(fields []ui.CardField) {
	states := make(map[string]string, len(fields))
	for _, field := range fields {
		states[field.Label] = field.Value
	}
	bridge := states["Docker 流量放行"]
	if bridge == "" {
		bridge = "入站" + states["Docker 入站放行"] + " · 出站" + states["Docker 出站放行"]
	} else {
		bridge = ui.MutedText(bridge)
	}
	mark := states["Docker 放行标记"]
	if mark != ui.PrimaryText("已生效") {
		mark = ui.MutedText(mark)
	}
	ui.MenuOptionStatusHint("6", "Docker 流量放行", "["+bridge+"]", "分别设置 Docker 网桥入站和出站")
	ui.MenuOptionStatusHint("7", "Docker 放行标记", "["+mark+"]", "让 Docker 接受 ServerTool 已放行的转发")
}

// dockerChoice returns the action and its error text for a Docker menu choice.
func dockerChoice(choice string, bridge, mark func() error) (func() error, string) {
	switch choice {
	case "6":
		return bridge, "设置 Docker 流量放行失败，已返回转发规则管理"
	case "7":
		return mark, "设置 Docker 放行标记失败，已返回转发规则管理"
	default:
		return nil, ""
	}
}
