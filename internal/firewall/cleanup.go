package firewall

import (
	"snail_tool/internal/firewall/configuration/restore"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

func cleanupService() restore.Service {
	return restore.Service{
		Store: configStore(), IsRoot: isRoot, CommandExists: commandExists,
		HasManagedConfig: hasManagedConfig, ReadOriginal: readOriginalState, Autostart: autostartService(),
	}
}

func Cleanup(_ *system.Account) error { return cleanupService().Run() }

// CleanupWithConfirm is the dedicated action under the configuration cleanup menu.
func CleanupWithConfirm(view *ui.UI) error { return cleanupService().RunWithConfirm(view) }
