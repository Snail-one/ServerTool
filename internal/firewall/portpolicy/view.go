package portpolicy

import (
	"fmt"
	"strings"

	"snail_tool/internal/ui"
)

type Report struct {
	Found                    bool
	Policy, Problem          string
	OtherInput, System       []string
	Private                  [2][]string
	Ports                    [2][2][2]string
	Exceptions, SystemFields []ui.CardField
}

func show(load func() (Report, error), showDockerPorts func()) error {
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "端口策略")
	summary, err := load()
	if err != nil {
		return err
	}
	defer showDockerPorts()
	if !summary.Found {
		ui.PrintSectionCard("未加载本工具防火墙规则", ui.CardField{Label: "本工具表", Value: "未找到 inet snail；不能据此判断服务器端口全部开放或关闭"})
		return nil
	}
	policy := "无法识别"
	if summary.Policy == "drop" {
		policy = ui.PrimaryText("默认丢弃（drop）")
	}
	if summary.Policy == "accept" {
		policy = "默认放行（accept）"
	}
	fields := []ui.CardField{
		{Label: "数据来源", Value: "当前内核已加载规则"},
		{Label: "汇总范围", Value: "inet snail 的 input 分流规则；新入站 TCP / UDP 业务流量"},
		{Label: "默认策略", Value: policy},
	}
	if len(summary.OtherInput) > 0 {
		fields = append(fields, ui.CardField{Label: "其他入站链", Value: strings.Join(summary.OtherInput, "；"), Detail: "这些链仍可能进一步限制访问"})
	}
	ui.PrintSectionCard("已加载端口策略", fields...)
	if summary.Problem != "" {
		ui.PrintSectionCard("无法完整汇总端口",
			ui.CardField{Label: "原因", Value: summary.Problem},
			ui.CardField{Label: "查看详情", Value: "返回后选择 5 查看全部已加载规则"},
		)
		return nil
	}
	ui.PrintSectionCard("来源划分",
		ui.CardField{Label: "内网 IPv4", Value: strings.Join(summary.Private[0], ", ")},
		ui.CardField{Label: "内网 IPv6", Value: strings.Join(summary.Private[1], ", ")},
		ui.CardField{Label: "公网", Value: "上述内网来源之外的地址"},
	)
	if len(summary.Exceptions) > 0 {
		fields := []ui.CardField{{Label: "匹配方式", Value: "优先于公网 / 内网基础端口"}}
		fields = append(fields, summary.Exceptions...)
		ui.PrintSectionCard("规则配置", fields...)
	}
	for scope, label := range []string{"公网", "内网"} {
		var ports []ui.CardField
		for family, version := range []string{"IPv4", "IPv6"} {
			for protocol, name := range []string{"TCP", "UDP"} {
				value := summary.Ports[scope][family][protocol]
				if scope == 1 && len(summary.Private[family]) == 0 {
					value = "没有匹配的内网来源"
				}
				ports = append(ports, ui.CardField{Label: label + " " + version + " " + name, Value: value})
			}
		}
		ui.PrintSectionCard(label+"放行端口", ports...)
	}
	system := summary.SystemFields
	if len(summary.System) > 0 {
		system = append(system, ui.CardField{Label: "其他系统流量", Value: strings.Join(summary.System, "、"), Detail: "具体条件见完整已加载规则"})
	}
	if len(system) > 0 {
		ui.PrintSectionCard("系统流量例外", system...)
	}
	fmt.Println("以上是本工具入站规则的放行范围；服务监听、其他规则及云安全组也会影响实际连通性。")
	return nil
}
