package firewall

import (
	"snail_tool/internal/firewall/configuration/ownership"
	"snail_tool/internal/firewall/settings"
	"snail_tool/internal/firewall/settings/autostart"
	"snail_tool/internal/firewall/settings/counters"
	"snail_tool/internal/firewall/settings/ipforwarding"
	"snail_tool/internal/firewall/settings/ping"
	"snail_tool/internal/firewall/settings/sshaccess"
	"snail_tool/internal/ui"
)

func runFeatureSettings(view *ui.UI) error {
	return (settings.Service{
		CommandExists: commandExists,
		CommandOutput: commandOutput,
		Autostart:     autostartService(),
		Ping:          pingService(),
		Counters:      counterService(),
		Forwarding:    ipForwardingService(),
		SSH:           sshAccessService(),
	}).Run(view)
}

func counterService() counters.Service {
	return counters.Service{Store: configStore(), IsRoot: isRoot, Validate: settings.ValidateCounterConfig}
}

func pingService() ping.Service {
	return ping.Service{
		Store: configStore(), IsRoot: isRoot, HasManagedConfig: ownership.HasManagedConfig, CommandExists: commandExists,
	}
}

func autostartService() autostart.Service {
	return autostart.Service{Store: configStore(), IsRoot: isRoot, CommandExists: commandExists, CommandRun: commandRun}
}

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

func sshAccessService() sshaccess.Service {
	return sshaccess.Service{
		Store: configStore(), IsRoot: isRoot, CommandExists: commandExists, HasManagedConfig: ownership.HasManagedConfig,
	}
}

// EnsureSSHPorts keeps firewall access to SSH ports during service reload.
func EnsureSSHPorts(ports []int) (bool, error) {
	return sshAccessService().EnsureSSHPorts(ports)
}
