package generate

import (
	"fmt"
	"strings"

	"snail_tool/internal/firewall/access"
	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/firewall/forward"
	"snail_tool/internal/firewall/forward/dockerbridge"
	"snail_tool/internal/firewall/forward/dockermark"
	rulespkg "snail_tool/internal/firewall/internal/rules"
	"snail_tool/internal/firewall/portmapping"
	autostartconfig "snail_tool/internal/firewall/settings/autostart"
	"snail_tool/internal/firewall/settings/counters"
	"snail_tool/internal/firewall/settings/ping"
	"snail_tool/internal/firewall/settings/sshaccess"
	"snail_tool/internal/log"
	"snail_tool/internal/ui"
)

type Service struct {
	Store                 configuration.Store
	IsRoot                func() bool
	EnsureNftables        func(*ui.UI) (bool, error)
	HasManagedConfig      func(string) bool
	Autostart             autostartconfig.Service
	SSH                   sshaccess.Service
	DockerMark            dockermark.Service
	ValidateCounterConfig func(string) error
}

func (service Service) Run(view *ui.UI) error {
	if !service.IsRoot() {
		return fmt.Errorf("防火墙配置需要 root 权限，请使用 sudo 运行本工具")
	}
	installed, err := service.EnsureNftables(view)
	if err != nil {
		return err
	}
	if !installed {
		return ui.ErrReturnToMenu
	}
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "生成配置")
	version, err := service.Store.Read()
	if err != nil {
		return err
	}
	firstConfiguration := !service.HasManagedConfig(string(version.Content))
	var firstAutostart *autostartconfig.Status
	autostartAction := "保持当前设置"
	if firstConfiguration {
		firstAutostart, err = service.Autostart.PrepareFirst()
		if err != nil {
			return err
		}
		autostartAction = "首次配置加载成功后自动开启"
		if firstAutostart == nil {
			autostartAction = "不支持自动设置（未找到 systemctl）"
		}
	}
	mappings, err := portmapping.Read(string(version.Content))
	if err != nil {
		return err
	}
	var preserved, preservedForward []rulespkg.Rule
	countersEnabled, err := counters.Enabled(string(version.Content))
	if err != nil {
		return err
	}
	pingEnabled, pingFound, err := ping.Read(string(version.Content))
	if err != nil {
		return err
	}
	dockerBridge, dockerBridgeFound, err := dockerbridge.Read(string(version.Content))
	if err != nil {
		return err
	}
	var droppedAccess, droppedForward int
	if strings.Contains(string(version.Content), rulespkg.AccessBegin) || strings.Contains(string(version.Content), rulespkg.AccessEnd) {
		preserved, err = rulespkg.ManagedRules(string(version.Content))
		if err != nil {
			return err
		}
		preserved, droppedAccess = OmitUnconvertedPortRules(preserved)
	}
	if strings.Contains(string(version.Content), rulespkg.ForwardBegin) || strings.Contains(string(version.Content), rulespkg.ForwardEnd) {
		preservedForward, err = rulespkg.ManagedRulesIn(string(version.Content), rulespkg.ForwardBlock)
		if err != nil {
			return err
		}
		preservedForward, droppedForward = OmitUnconvertedPortRules(preservedForward)
	}
	ports, err := service.SSH.DetectPorts()
	if err != nil {
		return err
	}
	if len(ports) > 1 {
		log.Warn("SSH 生效配置包含多个端口：", sshaccess.PortList(ports), "。通常建议只保留一个 SSH 端口。选择开放的范围会同时写入这些端口，请在 SSH 安全配置中检查并关闭不再使用的旧端口。")
	}
	sshFields := []ui.CardField{
		{Label: "端口", Value: sshaccess.PortList(ports), Detail: "/usr/sbin/sshd -T"},
		{Label: "放行协议", Value: "相同端口号同时放行 TCP / UDP，IPv4 / IPv6 同时应用"},
		{Label: "附加放行规则", Value: fmt.Sprintf("保留已有 %d 条规则", len(preserved))},
	}
	if text := unconvertedPortRuleText(droppedAccess, droppedForward); text != "" {
		sshFields = append(sshFields, ui.CardField{Label: "旧任意来源规则", Value: text})
	}
	if len(ports) > 1 {
		sshFields = append(sshFields, ui.CardField{Label: "SSH 提示", Value: "检测到多个 SSH 端口。选择开放的范围会同时写入这些端口；建议在 SSH 安全配置中检查并关闭不再使用的旧端口"})
	}
	options := rulespkg.Options{Store: service.Store, IsRoot: service.IsRoot, HasManagedConfig: service.HasManagedConfig}

	return configuration.Generate(view, configuration.Generation{
		Path:      service.Store.Path,
		SSHPorts:  sshaccess.PortList(ports),
		SSHFields: sshFields,
		Build: func(selection configuration.PortSelection) (string, error) {
			content, err := BuildScoped(ports, selection.PublicSSH, selection.PrivateSSH, selection.PublicWeb, selection.PrivateWeb)
			if err != nil {
				return "", err
			}
			content, err = rulespkg.ReplaceManagedRules(content, preserved)
			if err != nil {
				return "", err
			}
			content, err = rulespkg.ReplaceManagedRulesIn(content, preservedForward, rulespkg.ForwardBlock)
			if err != nil {
				return "", err
			}
			if version.Exists && !pingFound {
				content, err = ping.Remove(content)
			} else {
				content, err = ping.Replace(content, pingEnabled)
			}
			if err != nil {
				return "", err
			}
			content, err = portmapping.Replace(content, mappings)
			if err != nil {
				return "", err
			}
			content, err = counters.Replace(content, countersEnabled, service.ValidateCounterConfig)
			if err != nil {
				return "", err
			}
			if dockerBridgeFound {
				content, _, err = dockerbridge.Replace(content, dockerBridge)
				if err != nil {
					return "", err
				}
			}
			content, err = service.DockerMark.ApplyIfEnabled(content)
			if err != nil {
				return "", err
			}
			return content, nil
		},
		Review: configuration.Review{
			Path: service.Store.Path, Autostart: autostartAction,
			Legacy:  unconvertedPortRuleText(droppedAccess, droppedForward),
			Inspect: Inspect,
			EditAccess: func(view *ui.UI, content string) (string, error) {
				return access.Service(options).EditPolicyDraft(view, content, true, nil)
			},
			EditForward: func(view *ui.UI, content string) (string, error) {
				return forward.Service(options, dockerbridge.Service{}, service.DockerMark).EditPolicyDraft(view, content, true, nil)
			},
		},
		Save: func(content string) (string, error) {
			// Detect again after the interaction so a changed SSH listener cannot be
			// silently omitted from the generated firewall.
			latest, err := service.SSH.DetectPorts()
			if err != nil {
				return "", err
			}
			if sshaccess.PortList(latest) != sshaccess.PortList(ports) {
				return "", fmt.Errorf("SSH 配置端口已变化，请重新进入防火墙管理生成配置")
			}
			if firstAutostart != nil {
				if err := service.Autostart.Check(*firstAutostart); err != nil {
					return "", err
				}
			}
			saved, err := service.Store.SaveAndApply(version, content)
			if err != nil {
				if !saved {
					return "", err
				}
				return "", fmt.Errorf("配置已写入 %s，但未成功加载: %w", service.Store.Path, err)
			}
			if firstAutostart != nil {
				if err := service.Autostart.EnableFirst(*firstAutostart, []byte(content)); err != nil {
					return "", fmt.Errorf("防火墙配置已写入并加载，但自动开启开机自启未完成，请在常用功能设置中重试: %w", err)
				}
				return "已开启", nil
			} else if firstConfiguration {
				log.Warn("当前系统未找到 systemctl，防火墙配置已加载，未自动设置开机自启")
			}
			return autostartAction, nil
		},
	})
}

// Kind port is the old any-source port exception. Rebuild keeps source rules and
// full rules. An exception the user has not moved into the base port lists is omitted.
func OmitUnconvertedPortRules(rules []rulespkg.Rule) ([]rulespkg.Rule, int) {
	kept := make([]rulespkg.Rule, 0, len(rules))
	dropped := 0
	for _, rule := range rules {
		if rule.Kind == "port" {
			dropped++
			continue
		}
		kept = append(kept, rule)
	}
	return kept, dropped
}

func unconvertedPortRuleText(access, forward int) string {
	switch {
	case access > 0 && forward > 0:
		return fmt.Sprintf("未转换的访问 %d 条、转发 %d 条不保留，生成配置时覆盖", access, forward)
	case access > 0:
		return fmt.Sprintf("未转换的 %d 条不保留，生成配置时覆盖", access)
	case forward > 0:
		return fmt.Sprintf("未转换的转发 %d 条不保留，生成配置时覆盖", forward)
	default:
		return ""
	}
}

func Build(ports []int, publicWeb, privateWeb bool) (string, error) {
	return BuildScoped(ports, true, true, publicWeb, privateWeb)
}

func BuildScoped(ports []int, publicSSH, privateSSH, publicWeb, privateWeb bool) (string, error) {
	ports, err := sshaccess.MergePorts(ports)
	if err != nil {
		return "", err
	}
	content, clearPublic, clearPrivate := configuration.RenderTemplate(sshaccess.PortList(ports), configuration.PortSelection{
		PublicSSH: publicSSH, PrivateSSH: privateSSH, PublicWeb: publicWeb, PrivateWeb: privateWeb,
	})
	if clearPublic {
		content, err = clearScopeElements(content, true)
		if err != nil {
			return "", err
		}
	}
	if clearPrivate {
		content, err = clearScopeElements(content, false)
		if err != nil {
			return "", err
		}
	}
	return ping.Replace(content, false)
}

func clearScopeElements(content string, public bool) (string, error) {
	for _, set := range configuration.InputPortSets(public) {
		updated, err := configuration.ReplaceNamedSetElements(content, set.Name, "")
		if err != nil {
			return "", err
		}
		content = updated
	}
	return content, nil
}

func Inspect(content string) (configuration.Preview, error) {
	extraRules, err := rulespkg.ManagedRules(content)
	if err != nil {
		return configuration.Preview{}, err
	}
	pingEnabled, pingFound, err := ping.Read(content)
	if err != nil {
		return configuration.Preview{}, err
	}
	pingState := "沿用原有规则"
	if pingFound {
		pingState = ping.Label(pingEnabled)
	}
	counters, err := counters.Enabled(content)
	if err != nil {
		return configuration.Preview{}, err
	}
	counterState := "已关闭"
	if counters {
		counterState = ui.PrimaryText("已开启")
	}
	ssh, err := sshaccess.PortText(content)
	if err != nil {
		return configuration.Preview{}, err
	}
	return configuration.Preview{
		SSH: ssh, ExtraRules: len(extraRules), Ping: pingState, Counters: counterState,
		Access: access.SummaryFields(content), Forward: forward.SummaryFields(content),
	}, nil
}
