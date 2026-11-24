package firewall

import "snail_tool/internal/firewall/settings/forwarding"

var (
	forwardingProcRoot   = forwarding.DefaultProcRoot
	forwardingConfigPath = forwarding.DefaultConfigPath
)

func forwardingService() forwarding.Service {
	return forwarding.Service{
		ProcRoot: forwardingProcRoot, ConfigPath: forwardingConfigPath,
		IsRoot: isRoot, CommandExists: commandExists, CommandOutput: commandOutput,
	}
}
