package dockerbridge

import (
	"errors"
	"fmt"

	"snail_tool/internal/ui"
)

const InboundLabel = "Docker 入站放行"
const OutboundLabel = "Docker 出站放行"

type State struct {
	Inbound  bool
	Outbound bool
}

type Snapshot struct {
	State   State
	Prepare func(State) (func() (bool, error), error)
}

func (settings State) Fields() []ui.CardField {
	return []ui.CardField{
		{Label: InboundLabel, Value: dockerBridgeLabel(settings.Inbound)},
		{Label: OutboundLabel, Value: dockerBridgeLabel(settings.Outbound)},
	}
}

func dockerBridgeLabel(enabled bool) string {
	if enabled {
		return ui.StateText("已开启", true)
	}
	return ui.StateText("已关闭", false)
}

func dockerBridgeAction(enabled bool) string {
	if enabled {
		return "关闭"
	}
	return "开启"
}

func run(view *ui.UI, load func() (Snapshot, error), requireDockerPresent func() error) error {
	for {
		snapshot, err := load()
		if err != nil {
			return err
		}
		current := snapshot.State
		ui.ClearScreen()
		ui.MenuTitle("防火墙管理", "转发规则管理", "Docker 流量放行")
		ui.PrintInfoCard("Docker 流量放行", append(current.Fields(),
			ui.CardField{Label: "入站", Value: "转发到 Docker 网桥，例如访问已发布的容器端口"},
			ui.CardField{Label: "出站", Value: "从 Docker 网桥发出的转发，例如容器访问外网"},
			ui.CardField{Label: "匹配网桥", Value: "docker0、docker_gwbridge、br-*"},
			ui.CardField{Label: "开启后", Value: "匹配方向在转发端口名单前放行，随后由 Docker 的防火墙规则处理"},
		)...)
		ui.MenuOptionStatus("1", dockerBridgeAction(current.Inbound)+" "+InboundLabel, ui.SwitchBadge(current.Inbound))
		ui.MenuOptionStatus("2", dockerBridgeAction(current.Outbound)+" "+OutboundLabel, ui.SwitchBadge(current.Outbound))
		ui.MenuExit("0/q", "返回转发规则管理")
		choice, err := ui.AskChoice(view, "请选择", "", map[string]string{"1": "inbound", "2": "outbound"})
		if err != nil {
			return err
		}
		next := current
		direction, previouslyEnabled := InboundLabel, current.Inbound
		if choice == "inbound" {
			next.Inbound = !current.Inbound
		} else {
			direction, previouslyEnabled = OutboundLabel, current.Outbound
			next.Outbound = !current.Outbound
		}
		if !previouslyEnabled {
			if err := requireDockerPresent(); err != nil {
				return err
			}
		}
		save, err := snapshot.Prepare(next)
		if err != nil {
			return err
		}
		action := dockerBridgeAction(previouslyEnabled)
		ui.ClearScreen()
		ui.MenuTitle("防火墙管理", "转发规则管理", "Docker 流量放行", "确认变更")
		ui.PrintInfoCard("即将应用的 Docker 流量放行设置", append(next.Fields(),
			ui.CardField{Label: "操作", Value: action + " " + direction},
			ui.CardField{Label: "生效方式", Value: "保存并加载整份当前配置，包括其他待加载修改"},
		)...)
		confirmed, err := ui.ConfirmMenuChange(view, "确认"+action+" "+direction+"并加载当前配置？", "转发规则管理")
		if errors.Is(err, ui.ErrReturnToMenu) {
			continue
		}
		if err != nil {
			return err
		}
		if !confirmed {
			return ui.ErrReturnToMenu
		}
		saved, err := save()
		if err != nil {
			if !saved {
				return err
			}
			return fmt.Errorf("Docker 流量放行配置已保存，但未成功加载；原配置已备份，可输入 r 重试: %w", err)
		}
		ui.PrintSuccessCard(direction+"已"+action, append(next.Fields(),
			ui.CardField{Label: "生效状态", Value: "配置已保存并加载"},
		)...)
		return nil
	}
}
