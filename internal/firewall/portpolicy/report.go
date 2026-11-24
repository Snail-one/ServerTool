package portpolicy

import (
	"fmt"
	"strings"

	"snail_tool/internal/ui"
)

type Service struct {
	CommandExists func(string) bool
	CommandOutput func(string, ...string) (string, error)
}

func (service Service) Show() error {
	return show(service.readLoadedPortReport, service.showDockerPorts)
}

func (service Service) readLoadedPortReport() (Report, error) {
	output, err := service.CommandOutput("nft", "-j", "list", "ruleset")
	if err != nil {
		return Report{}, fmt.Errorf("读取当前端口规则失败: %w: %s", err, strings.TrimSpace(output))
	}
	summary, err := summarizeLoadedPorts(output)
	if err != nil {
		return Report{}, fmt.Errorf("解析当前端口规则失败: %w", err)
	}
	report := Report{
		Found: summary.found, Policy: summary.policy, Problem: summary.problem,
		OtherInput: summary.otherInput, System: summary.system,
		Private: summary.private,
	}
	if !summary.found || summary.problem != "" {
		return report, nil
	}
	for scope := range report.Ports {
		for family := range report.Ports[scope] {
			for protocol := range report.Ports[scope][family] {
				report.Ports[scope][family][protocol] = summary.ports[scope][family][protocol].String()
			}
		}
	}
	var exceptions, system []ui.CardField
	for _, rule := range mergePortExceptions(summary.exceptions) {
		if rule.families == 2 && rule.protocols == 2 && rule.sources == nil && rule.hasPorts && rule.hasSourcePorts && rule.ports.String() == "546" && rule.sourcePorts.String() == "547" {
			system = append(system, ui.CardField{Label: "DHCPv6 客户端", Value: rule.description()})
		} else {
			label := "放行"
			if rule.verdict == "drop" {
				label = "拒绝"
			}
			exceptions = append(exceptions, ui.CardField{Label: label, Value: rule.description()})
		}
	}
	report.Exceptions, report.SystemFields = exceptions, system
	return report, nil
}
