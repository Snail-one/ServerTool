package firewall

import "snail_tool/internal/firewall/settings/ipforwarding"

var (
	forwardingProcRoot   = ipforwarding.DefaultProcRoot
	forwardingConfigPath = ipforwarding.DefaultConfigPath
)

func ipForwardingService() ipforwarding.Service {
	return ipforwarding.Service{
		ProcRoot: forwardingProcRoot, ConfigPath: forwardingConfigPath,
		IsRoot: isRoot, CommandExists: commandExists, CommandOutput: commandOutput,
	}
}
