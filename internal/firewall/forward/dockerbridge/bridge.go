package dockerbridge

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/firewall/settings/counters"
	"snail_tool/internal/ui"
)

type Service struct {
	Store            configuration.Store
	IsRoot           func() bool
	HasManagedConfig func(string) bool
	RequireDocker    func() error
}

const (
	Begin                   = "# ===== BEGIN SNAIL DOCKER FORWARD ====="
	End                     = "# ===== END SNAIL DOCKER FORWARD ====="
	legacyJump              = "jump forward_docker"
	nextRule                = "ip saddr $snail_forward_private_ipv4 goto forward_private_ports"
	dockerBridgeNeedsConfig = "请先返回防火墙管理，选择 d 生成本工具防火墙配置，再设置 Docker 流量放行"
)

func dockerBridgeRules(settings State, counters bool) []string {
	var rules []string
	verdict := "accept"
	if counters {
		verdict = "counter accept"
	}
	for _, direction := range []struct {
		enabled bool
		match   string
	}{
		{settings.Inbound, "oifname"},
		{settings.Outbound, "iifname"},
	} {
		if direction.enabled {
			for _, bridge := range []string{"docker0", "docker_gwbridge", "br-*"} {
				rules = append(rules, fmt.Sprintf("%s %q %s", direction.match, bridge, verdict))
			}
		}
	}
	return rules
}

func Read(content string) (settings State, found bool, err error) {
	begins, ends := strings.Count(content, Begin), strings.Count(content, End)
	if begins == 0 && ends == 0 {
		return settings, false, nil
	}
	begin, end := strings.Index(content, Begin), strings.Index(content, End)
	if begins != 1 || ends != 1 || begin > end {
		return settings, false, fmt.Errorf("Docker 流量放行开关标记损坏，未修改")
	}
	jumpAt := strings.Index(content, "jump forward_source_allow")
	routeAt := strings.Index(content, nextRule)
	if jumpAt < 0 || routeAt < 0 || begin < jumpAt || end+len(End) > routeAt {
		return settings, false, fmt.Errorf("Docker 流量放行开关不在转发链的来源放行和端口名单之间，未修改")
	}
	body, err := counters.WithoutStatements(content[begin+len(Begin) : end])
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
	if slices.Equal(rules, []string{legacyJump}) {
		return State{Inbound: true, Outbound: true}, true, nil
	}
	for _, candidate := range []State{
		{}, {Inbound: true}, {Outbound: true}, {Inbound: true, Outbound: true},
	} {
		if slices.Equal(rules, dockerBridgeRules(candidate, false)) {
			return candidate, true, nil
		}
	}
	return settings, false, fmt.Errorf("Docker 流量放行开关内容已被手动修改，无法确认状态，未覆盖")
}

func Replace(content string, settings State) (string, bool, error) {
	current, found, err := Read(content)
	if err != nil {
		return "", false, err
	}
	if current == settings {
		return content, false, nil
	}
	counters, err := counters.Enabled(content)
	if err != nil {
		return "", false, err
	}
	lines := append([]string{Begin}, dockerBridgeRules(settings, counters)...)
	block := strings.Join(append(lines, End), "\n        ")
	if found {
		begin := strings.Index(content, Begin)
		end := strings.Index(content, End) + len(End)
		return content[:begin] + block + content[end:], true, nil
	}
	at := strings.Index(content, nextRule)
	source := strings.Index(content, "jump forward_source_allow")
	if source < 0 || at <= source {
		return "", false, errors.New(dockerBridgeNeedsConfig)
	}
	lineStart := strings.LastIndex(content[:at], "\n") + 1
	return content[:lineStart] + "        " + block + "\n" + content[lineStart:], true, nil
}

func (service Service) StateFields(content string) []ui.CardField {
	if !service.HasManagedConfig(content) {
		return []ui.CardField{
			{Label: InboundLabel, Value: ui.StateText("未配置", false)},
			{Label: OutboundLabel, Value: ui.StateText("未配置", false)},
		}
	}
	settings, _, err := Read(content)
	if err != nil {
		return []ui.CardField{{Label: "Docker 流量放行", Value: "无法判断", Detail: err.Error()}}
	}
	return settings.Fields()
}

func (service Service) Run(view *ui.UI) error {
	if !service.IsRoot() {
		return fmt.Errorf("设置 Docker 流量放行需要 root 权限，请使用 sudo 运行本工具")
	}
	return run(view, service.readSnapshot, service.RequireDocker)
}

func (service Service) readSnapshot() (Snapshot, error) {
	version, err := service.Store.Read()
	if err != nil {
		return Snapshot{}, err
	}
	content := string(version.Content)
	if !version.Exists || !service.HasManagedConfig(content) || !strings.Contains(content, "hook forward") {
		return Snapshot{}, errors.New(dockerBridgeNeedsConfig)
	}
	current, _, err := Read(content)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{State: current, Prepare: func(next State) (func() (bool, error), error) {
		updated, _, err := Replace(content, next)
		if err != nil {
			return nil, err
		}
		return func() (bool, error) { return service.Store.SaveAndApply(version, updated) }, nil
	}}, nil
}
