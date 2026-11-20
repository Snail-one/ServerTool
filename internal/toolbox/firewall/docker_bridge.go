package firewall

import (
	"errors"
	"fmt"
	"strings"

	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

const (
	dockerBridgeBegin = "# ===== BEGIN SNAIL DOCKER FORWARD ====="
	dockerBridgeEnd   = "# ===== END SNAIL DOCKER FORWARD ====="
	dockerBridgeJump  = "jump forward_docker"
)

func dockerBridgeBlock(enabled bool) string {
	if !enabled {
		return dockerBridgeBegin + "\n        " + dockerBridgeEnd
	}
	return dockerBridgeBegin + "\n        " + dockerBridgeJump + "\n        " + dockerBridgeEnd
}

func dockerBridgeLabel(enabled bool) string {
	if enabled {
		return ui.PrimaryText("已开启")
	}
	return "已关闭"
}

func readDockerBridge(content string) (enabled, found bool, err error) {
	begins, ends := strings.Count(content, dockerBridgeBegin), strings.Count(content, dockerBridgeEnd)
	if begins == 0 && ends == 0 {
		return false, false, nil
	}
	if begins != 1 || ends != 1 {
		return false, false, fmt.Errorf("Docker 转发开关标记损坏，未修改")
	}
	begin, end := strings.Index(content, dockerBridgeBegin), strings.Index(content, dockerBridgeEnd)
	if begin > end {
		return false, false, fmt.Errorf("Docker 转发开关标记损坏，未修改")
	}
	jumpAt := strings.Index(content, "jump forward_source_allow")
	routeAt := strings.Index(content, "ip saddr $snail_forward_private_ipv4 goto forward_private_ports")
	if jumpAt < 0 || routeAt < 0 || begin < jumpAt || end+len(dockerBridgeEnd) > routeAt {
		return false, false, fmt.Errorf("Docker 转发开关不在转发链的来源放行和端口名单之间，未修改")
	}
	tokens, err := configTokens(content[begin+len(dockerBridgeBegin) : end])
	if err != nil {
		return false, false, err
	}
	switch {
	case len(tokens) == 0:
		return false, true, nil
	case len(tokens) == 2 && tokens[0].text == "jump" && tokens[1].text == "forward_docker":
		return true, true, nil
	default:
		return false, false, fmt.Errorf("Docker 转发开关内容已被手动修改，无法确认状态，未覆盖")
	}
}

func withDockerBridge(content string, enabled bool) (string, bool, error) {
	current, found, err := readDockerBridge(content)
	if err != nil {
		return "", false, err
	}
	if found && current == enabled || !found && !enabled {
		return content, false, nil
	}
	block := dockerBridgeBlock(enabled)
	if found {
		begin := strings.Index(content, dockerBridgeBegin)
		end := strings.Index(content, dockerBridgeEnd) + len(dockerBridgeEnd)
		return content[:begin] + block + content[end:], true, nil
	}
	route := "ip saddr $snail_forward_private_ipv4 goto forward_private_ports"
	at := strings.Index(content, route)
	if at < 0 {
		return "", false, fmt.Errorf("请先返回防火墙管理，选择 7 生成本工具防火墙配置，再设置 Docker 转发")
	}
	lineStart := strings.LastIndex(content[:at], "\n") + 1
	return content[:lineStart] + "        " + block + "\n" + content[lineStart:], true, nil
}

func dockerBridgeStateField() ui.CardField {
	field := ui.CardField{Label: "Docker 转发"}
	version, err := readConfigVersion()
	if err != nil {
		field.Value = "无法判断"
		field.Detail = err.Error()
		return field
	}
	if !version.exists || !hasManagedConfig(string(version.content)) {
		field.Value = "未配置"
		return field
	}
	enabled, _, err := readDockerBridge(string(version.content))
	if err != nil {
		field.Value = "无法判断"
		field.Detail = err.Error()
		return field
	}
	field.Value = dockerBridgeLabel(enabled)
	return field
}

func configureDockerBridge(view *ui.UI) error {
	if !isRoot() {
		return fmt.Errorf("设置 Docker 转发需要 root 权限，请使用 sudo 运行本工具")
	}
	for {
		version, err := readConfigVersion()
		if err != nil {
			return err
		}
		if !version.exists || !hasManagedConfig(string(version.content)) || !strings.Contains(string(version.content), "hook forward") {
			return fmt.Errorf("请先返回防火墙管理，选择 7 生成本工具防火墙配置，再设置 Docker 转发")
		}
		enabled, _, err := readDockerBridge(string(version.content))
		if err != nil {
			return err
		}
		ui.ClearScreen()
		ui.MenuTitle("防火墙管理", "常用功能设置", "Docker 转发")
		ui.PrintInfoCard("Docker 转发",
			ui.CardField{Label: "当前", Value: dockerBridgeLabel(enabled)},
			ui.CardField{Label: "默认", Value: "关闭"},
			ui.CardField{Label: "匹配", Value: "docker0、docker_gwbridge、br-*"},
			ui.CardField{Label: "开启后", Value: "这些网卡的转发不再走端口名单，随后仍由 Docker 自己的防火墙决定"},
			ui.CardField{Label: "不影响", Value: "入站规则、转发端口名单、连接标记和 DOCKER-USER"},
		)
		ui.MenuOption("1", "开启")
		ui.MenuOption("2", "关闭")
		ui.MenuExit("0/q", "返回常用功能设置")
		choice, err := askRuleChoice(view, "请选择", "", map[string]string{"1": "on", "2": "off"})
		if err != nil {
			return err
		}
		target := choice == "on"
		updated, changed, err := withDockerBridge(string(version.content), target)
		if err != nil {
			return err
		}
		action := "关闭"
		if target {
			action = "开启"
		}
		if !changed {
			ui.PrintInfoCard("无需修改", ui.CardField{Label: "Docker 转发", Value: dockerBridgeLabel(enabled)})
			view.Pause()
			continue
		}
		ui.ClearScreen()
		ui.MenuTitle("防火墙管理", "常用功能设置", "Docker 转发", "确认变更")
		ui.PrintInfoCard("即将应用的 Docker 转发设置",
			ui.CardField{Label: "当前", Value: dockerBridgeLabel(enabled)},
			ui.CardField{Label: "操作", Value: action + " Docker 网桥转发放行"},
			ui.CardField{Label: "生效方式", Value: "保存并加载整份当前配置，包括其他待加载修改"},
		)
		confirmed, err := view.Confirm("确认" + action + " Docker 网桥转发放行并加载当前配置？(y/N，0/q 返回)：")
		if errors.Is(err, shared.ErrReturnToMenu) {
			continue
		}
		if err != nil {
			return err
		}
		if !confirmed {
			return shared.ErrReturnToMenu
		}
		if err := writeConfig(updated, version); err != nil {
			return err
		}
		if err := applyConfig([]byte(updated)); err != nil {
			return fmt.Errorf("Docker 转发配置已保存，但未成功加载；原配置已备份，可输入 r 重试: %w", err)
		}
		ui.PrintSuccessCard("Docker 转发已"+action,
			ui.CardField{Label: "当前", Value: dockerBridgeLabel(target)},
			ui.CardField{Label: "生效状态", Value: "配置已保存并加载"},
		)
		return nil
	}
}
