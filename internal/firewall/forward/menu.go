package forward

import (
	"snail_tool/internal/ui"
)

const Title = "转发规则管理"
const Label = "转发规则"

func printDockerOptions() {
	ui.MenuOptionHint("6", "Docker 流量放行", "分别设置 Docker 网桥入站和出站")
	ui.MenuOptionHint("7", "Docker 放行标记", "让 Docker 接受 ServerTool 已放行的转发")
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
