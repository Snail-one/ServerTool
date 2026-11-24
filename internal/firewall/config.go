package firewall

import (
	"fmt"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/firewall/configuration/generate"
	"snail_tool/internal/storage"
	"snail_tool/internal/ui"
)

var (
	configPath = "/etc/nftables.conf"
	dataDir    = storage.DataDir
)

func configStore() configuration.Store {
	return configuration.Store{Path: configPath, DataDir: dataDir, CommandOutput: commandOutput, PreserveOriginal: preserveOriginal}
}

func editConfig() error {
	return configStore().Edit(commandExists, commandRun)
}

func loadConfig(view *ui.UI) error {
	return configStore().Reload(view, isRoot, func(content []byte) (bool, error) {
		if !commandExists("unshare") {
			return false, fmt.Errorf("缺少 unshare，无法对比配置与内核规则")
		}
		return compareConfigRules(content, inspectLiveRules())
	})
}

func configure(view *ui.UI) error {
	return (generate.Service{
		Store: configStore(), IsRoot: isRoot, EnsureNftables: ensureNftables, HasManagedConfig: hasManagedConfig,
		Autostart: autostartService(), SSH: sshAccessService(), DockerMark: dockerMarkService(), ValidateCounterConfig: validateCounterConfig,
	}).Run(view)
}
