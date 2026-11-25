package firewall

import (
	"snail_tool/internal/firewall/settings/autostart"
	"snail_tool/internal/firewall/status"
	"snail_tool/internal/ui"
)

func currentConfigFields(boot autostart.Status) []ui.CardField {
	return (status.Service{
		Store: configStore(), CommandExists: commandExists, Forwarding: ipForwardingService(),
	}).Fields(boot)
}
