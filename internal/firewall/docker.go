package firewall

import (
	"snail_tool/internal/firewall/configuration/ownership"
	"snail_tool/internal/firewall/forward/dockerbridge"
	"snail_tool/internal/firewall/forward/dockermark"
)

var dockerForwardUnitPath = dockermark.DefaultUnitPath

func dockerMarkService() dockermark.Service {
	return dockermark.Service{
		Store: configStore(), UnitPath: dockerForwardUnitPath,
		IsRoot: isRoot, CommandExists: commandExists, HasManagedConfig: ownership.HasManagedConfig,
	}
}

func dockerBridgeService() dockerbridge.Service {
	return dockerbridge.Service{
		Store: configStore(), IsRoot: isRoot, HasManagedConfig: ownership.HasManagedConfig,
		RequireDocker: dockerMarkService().RequireDocker,
	}
}

func SyncDockerForward() error { return dockerMarkService().Sync() }

func RemoveDockerForwardService() error { return dockerMarkService().RemoveService() }
