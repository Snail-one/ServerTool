package firewall

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

const (
	dockerBridgeBegin       = "# ===== BEGIN SNAIL DOCKER FORWARD ====="
	dockerBridgeEnd         = "# ===== END SNAIL DOCKER FORWARD ====="
	dockerBridgeLegacyJump  = "jump forward_docker"
	dockerBridgeNextRule    = "ip saddr $snail_forward_private_ipv4 goto forward_private_ports"
	dockerBridgeInbound     = "Docker 入站放行"
	dockerBridgeOutbound    = "Docker 出站放行"
	dockerBridgeNeedsConfig = "请先返回防火墙管理，选择 d 生成本工具防火墙配置，再设置 Docker 流量放行"
)

type dockerBridgeSettings struct {
	inbound  bool // The packet leaves the host's forward chain toward a Docker bridge.
	outbound bool // The packet arrives at the host's forward chain from a Docker bridge.
}

func dockerBridgeRules(settings dockerBridgeSettings, counters bool) []string {
	var rules []string
	verdict := "accept"
	if counters {
		verdict = "counter accept"
	}
	for _, direction := range []struct {
		enabled bool
		match   string
	}{
		{settings.inbound, "oifname"},
		{settings.outbound, "iifname"},
	} {
		if direction.enabled {
			for _, bridge := range []string{"docker0", "docker_gwbridge", "br-*"} {
				rules = append(rules, fmt.Sprintf("%s %q %s", direction.match, bridge, verdict))
			}
		}
	}
	return rules
}

func dockerBridgeLabel(enabled bool) string {
	if enabled {
		return ui.PrimaryText("已开启")
	}
	return "已关闭"
}

func readDockerBridge(content string) (settings dockerBridgeSettings, found bool, err error) {
	begins, ends := strings.Count(content, dockerBridgeBegin), strings.Count(content, dockerBridgeEnd)
	if begins == 0 && ends == 0 {
		return settings, false, nil
	}
	begin, end := strings.Index(content, dockerBridgeBegin), strings.Index(content, dockerBridgeEnd)
	if begins != 1 || ends != 1 || begin > end {
		return settings, false, fmt.Errorf("Docker 流量放行开关标记损坏，未修改")
	}
	jumpAt := strings.Index(content, "jump forward_source_allow")
	routeAt := strings.Index(content, dockerBridgeNextRule)
	if jumpAt < 0 || routeAt < 0 || begin < jumpAt || end+len(dockerBridgeEnd) > routeAt {
		return settings, false, fmt.Errorf("Docker 流量放行开关不在转发链的来源放行和端口名单之间，未修改")
	}
	body, err := withoutCounterStatements(content[begin+len(dockerBridgeBegin) : end])
	if err != nil {
		return settings, false, err
	}
	var rules []string
	for _, line := range strings.Split(body, "\n") {
		line, _, _ = strings.Cut(line, "#")
		line = strings.TrimSuffix(strings.TrimSpace(line), ";")
		if fields := strings.Fields(line); len(fields) > 0 {
			rules = append(rules, strings.Join(fields, " "))
		}
	}
	if slices.Equal(rules, []string{dockerBridgeLegacyJump}) {
		return dockerBridgeSettings{inbound: true, outbound: true}, true, nil
	}
	for _, candidate := range []dockerBridgeSettings{
		{}, {inbound: true}, {outbound: true}, {inbound: true, outbound: true},
	} {
		if slices.Equal(rules, dockerBridgeRules(candidate, false)) {
			return candidate, true, nil
		}
	}
	return settings, false, fmt.Errorf("Docker 流量放行开关内容已被手动修改，无法确认状态，未覆盖")
}

func withDockerBridge(content string, settings dockerBridgeSettings) (string, bool, error) {
	current, found, err := readDockerBridge(content)
	if err != nil {
		return "", false, err
	}
	if current == settings {
		return content, false, nil
	}
	counters, err := packetCountersEnabled(content)
	if err != nil {
		return "", false, err
	}
	lines := append([]string{dockerBridgeBegin}, dockerBridgeRules(settings, counters)...)
	block := strings.Join(append(lines, dockerBridgeEnd), "\n        ")
	if found {
		begin := strings.Index(content, dockerBridgeBegin)
		end := strings.Index(content, dockerBridgeEnd) + len(dockerBridgeEnd)
		return content[:begin] + block + content[end:], true, nil
	}
	at := strings.Index(content, dockerBridgeNextRule)
	source := strings.Index(content, "jump forward_source_allow")
	if source < 0 || at <= source {
		return "", false, errors.New(dockerBridgeNeedsConfig)
	}
	lineStart := strings.LastIndex(content[:at], "\n") + 1
	return content[:lineStart] + "        " + block + "\n" + content[lineStart:], true, nil
}

func (settings dockerBridgeSettings) fields() []ui.CardField {
	return []ui.CardField{
		{Label: dockerBridgeInbound, Value: dockerBridgeLabel(settings.inbound)},
		{Label: dockerBridgeOutbound, Value: dockerBridgeLabel(settings.outbound)},
	}
}

func dockerBridgeStateFields(content string) []ui.CardField {
	if !hasManagedConfig(content) {
		return []ui.CardField{
			{Label: dockerBridgeInbound, Value: "未配置"},
			{Label: dockerBridgeOutbound, Value: "未配置"},
		}
	}
	settings, _, err := readDockerBridge(content)
	if err != nil {
		return []ui.CardField{{Label: "Docker 流量放行", Value: "无法判断", Detail: err.Error()}}
	}
	return settings.fields()
}

func dockerBridgeAction(enabled bool) string {
	if enabled {
		return "关闭"
	}
	return "开启"
}

func configureDockerBridge(view *ui.UI) error {
	if !isRoot() {
		return fmt.Errorf("设置 Docker 流量放行需要 root 权限，请使用 sudo 运行本工具")
	}
	for {
		version, err := readConfigVersion()
		if err != nil {
			return err
		}
		content := string(version.content)
		if !version.exists || !hasManagedConfig(content) || !strings.Contains(content, "hook forward") {
			return errors.New(dockerBridgeNeedsConfig)
		}
		current, _, err := readDockerBridge(content)
		if err != nil {
			return err
		}
		ui.ClearScreen()
		ui.MenuTitle("防火墙管理", "转发规则管理", "Docker 流量放行")
		ui.PrintInfoCard("Docker 流量放行", append(current.fields(),
			ui.CardField{Label: "入站", Value: "转发到 Docker 网桥，例如访问已发布的容器端口"},
			ui.CardField{Label: "出站", Value: "从 Docker 网桥发出的转发，例如容器访问外网"},
			ui.CardField{Label: "匹配网桥", Value: "docker0、docker_gwbridge、br-*"},
			ui.CardField{Label: "开启后", Value: "匹配方向在转发端口名单前放行，随后由 Docker 的防火墙规则处理"},
		)...)
		ui.MenuOption("1", dockerBridgeAction(current.inbound)+" "+dockerBridgeInbound)
		ui.MenuOption("2", dockerBridgeAction(current.outbound)+" "+dockerBridgeOutbound)
		ui.MenuExit("0/q", "返回转发规则管理")
		choice, err := askRuleChoice(view, "请选择", "", map[string]string{"1": "inbound", "2": "outbound"})
		if err != nil {
			return err
		}
		next := current
		direction, previouslyEnabled := dockerBridgeInbound, current.inbound
		if choice == "inbound" {
			next.inbound = !current.inbound
		} else {
			direction, previouslyEnabled = dockerBridgeOutbound, current.outbound
			next.outbound = !current.outbound
		}
		if !previouslyEnabled {
			if err := requireDockerPresent(); err != nil {
				return err
			}
		}
		updated, _, err := withDockerBridge(content, next)
		if err != nil {
			return err
		}
		action := dockerBridgeAction(previouslyEnabled)
		ui.ClearScreen()
		ui.MenuTitle("防火墙管理", "转发规则管理", "Docker 流量放行", "确认变更")
		ui.PrintInfoCard("即将应用的 Docker 流量放行设置", append(next.fields(),
			ui.CardField{Label: "操作", Value: action + " " + direction},
			ui.CardField{Label: "生效方式", Value: "保存并加载整份当前配置，包括其他待加载修改"},
		)...)
		confirmed, err := confirmMenuChange(view, "确认"+action+" "+direction+"并加载当前配置？", "转发规则管理")
		if errors.Is(err, shared.ErrReturnToMenu) {
			continue
		}
		if err != nil {
			return err
		}
		if !confirmed {
			return shared.ErrReturnToMenu
		}
		saved, err := saveAndApplyConfig(version, updated)
		if err != nil {
			if !saved {
				return err
			}
			return fmt.Errorf("Docker 流量放行配置已保存，但未成功加载；原配置已备份，可输入 r 重试: %w", err)
		}
		ui.PrintSuccessCard(direction+"已"+action, append(next.fields(),
			ui.CardField{Label: "生效状态", Value: "配置已保存并加载"},
		)...)
		return nil
	}
}
