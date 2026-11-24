package firewall

import (
	"snail_tool/internal/firewall/settings/sshaccess"
)

func sshAccessService() sshaccess.Service {
	return sshaccess.Service{
		Store: configStore(), IsRoot: isRoot, CommandExists: commandExists, HasManagedConfig: hasManagedConfig,
		Confirm: confirmFeatureChange,
	}
}

// EnsureSSHPorts keeps firewall access to SSH ports during service reload.
func EnsureSSHPorts(ports []int) (bool, error) {
	return sshAccessService().EnsureSSHPorts(ports)
}
