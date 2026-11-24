package firewall

import (
	"snail_tool/internal/firewall/configuration/generate"
	"snail_tool/internal/ui"
)

func configure(view *ui.UI) error {
	return (generate.Service{
		Store: configStore(), IsRoot: isRoot, EnsureNftables: ensureNftables, HasManagedConfig: hasManagedConfig,
		Autostart: autostartService(), SSH: sshAccessService(), DockerMark: dockerMarkService(), ValidateCounterConfig: validateCounterConfig,
	}).Run(view)
}
