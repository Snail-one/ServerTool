package firewall

import (
	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/firewall/configuration/ownership"
	"snail_tool/internal/firewall/configuration/restore"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

var preserveOriginal = saveOriginalState

func saveOriginalState() error {
	return restore.PreserveInitial(originalStore(), commandExists)
}

func readOriginalState() (*configuration.OriginalState, error) {
	return originalStore().ReadOriginal(ownership.HasManagedConfig)
}

func cleanupService() restore.Service {
	return restore.Service{
		Store: configStore(), IsRoot: isRoot, CommandExists: commandExists,
		HasManagedConfig: ownership.HasManagedConfig, ReadOriginal: readOriginalState, Autostart: autostartService(),
	}
}

func Cleanup(_ *system.Account) error { return cleanupService().Run() }

// CleanupWithConfirm is the dedicated action under the configuration cleanup menu.
func CleanupWithConfirm(view *ui.UI) error { return cleanupService().RunWithConfirm(view) }
