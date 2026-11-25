package firewall

import (
	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/firewall/configuration/generate"
	"snail_tool/internal/firewall/configuration/ownership"
	"snail_tool/internal/firewall/settings"
	"snail_tool/internal/storage"
	"snail_tool/internal/ui"
)

var (
	configPath = "/etc/nftables.conf"
	dataDir    = storage.DataDir
)

func originalStore() configuration.Store {
	return configuration.Store{Path: configPath, DataDir: dataDir, CommandOutput: commandOutput}
}

func configStore() configuration.Store {
	store := originalStore()
	store.PreserveOriginal = preserveOriginal
	return store
}

func editConfig() error {
	return configStore().Edit(commandExists, commandRun)
}

func loadConfig(view *ui.UI) error {
	return configStore().Reload(view, isRoot, commandExists)
}

func configure(view *ui.UI) error {
	return (generate.Service{
		Store: configStore(), IsRoot: isRoot, EnsureNftables: ensureNftables, HasManagedConfig: ownership.HasManagedConfig,
		Autostart: autostartService(), SSH: sshAccessService(), DockerMark: dockerMarkService(), ValidateCounterConfig: settings.ValidateCounterConfig,
	}).Run(view)
}
