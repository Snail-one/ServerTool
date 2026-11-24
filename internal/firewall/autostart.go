package firewall

import "snail_tool/internal/firewall/settings/autostart"

func autostartService() autostart.Service {
	return autostart.Service{Store: configStore(), IsRoot: isRoot, CommandExists: commandExists, CommandRun: commandRun}
}
