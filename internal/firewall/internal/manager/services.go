package manager

import (
	"snail_tool/internal/firewall/access"
	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/firewall/configuration/generate"
	"snail_tool/internal/firewall/configuration/ownership"
	"snail_tool/internal/firewall/configuration/restore"
	"snail_tool/internal/firewall/forward"
	"snail_tool/internal/firewall/forward/dockerbridge"
	"snail_tool/internal/firewall/forward/dockermark"
	"snail_tool/internal/firewall/internal/rules"
	"snail_tool/internal/firewall/nftables"
	"snail_tool/internal/firewall/settings"
	"snail_tool/internal/firewall/settings/autostart"
	"snail_tool/internal/firewall/settings/counters"
	"snail_tool/internal/firewall/settings/ipforwarding"
	"snail_tool/internal/firewall/settings/ping"
	"snail_tool/internal/firewall/settings/sshaccess"
	"snail_tool/internal/firewall/status"
	"snail_tool/internal/storage"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

// Environment holds the host commands and paths shared by firewall actions.
type Environment struct {
	CommandExists func(string) bool
	CommandRun    func(string, ...string) error
	CommandOutput func(string, ...string) (string, error)
	IsRoot        func() bool

	ConfigPath            string
	DataDir               string
	ForwardingProcRoot    string
	ForwardingConfigPath  string
	DockerForwardUnitPath string
	PreserveOriginal      func() error
}

func DefaultEnvironment() *Environment {
	return &Environment{
		CommandExists:         system.CommandExists,
		CommandRun:            system.Run,
		CommandOutput:         system.Output,
		IsRoot:                system.IsRoot,
		ConfigPath:            "/etc/nftables.conf",
		DataDir:               storage.DataDir,
		ForwardingProcRoot:    ipforwarding.DefaultProcRoot,
		ForwardingConfigPath:  ipforwarding.DefaultConfigPath,
		DockerForwardUnitPath: dockermark.DefaultUnitPath,
	}
}

func (env *Environment) ensureNftables(view *ui.UI) (bool, error) {
	return nftables.Ensure(view, env.CommandExists, env.CommandRun)
}

func (env *Environment) originalStore() configuration.Store {
	return configuration.Store{Path: env.ConfigPath, DataDir: env.DataDir, CommandOutput: env.CommandOutput}
}

func (env *Environment) Store() configuration.Store {
	store := env.originalStore()
	store.PreserveOriginal = env.PreserveOriginal
	if store.PreserveOriginal == nil {
		store.PreserveOriginal = env.SaveOriginal
	}
	return store
}

func (env *Environment) EditConfig() error {
	return env.Store().Edit(env.CommandExists, env.CommandRun)
}

func (env *Environment) LoadConfig(view *ui.UI) error {
	return env.Store().Reload(view, env.IsRoot, env.CommandExists)
}

func (env *Environment) Generate(view *ui.UI) error {
	return (generate.Service{
		Store: env.Store(), IsRoot: env.IsRoot, EnsureNftables: env.ensureNftables, HasManagedConfig: ownership.HasManagedConfig,
		Autostart: env.Autostart(), SSH: env.SSHAccess(), DockerMark: env.DockerMark(), ValidateCounterConfig: settings.ValidateCounterConfig,
	}).Run(view)
}

func (env *Environment) SaveOriginal() error {
	return restore.PreserveInitial(env.originalStore(), env.CommandExists)
}

func (env *Environment) ReadOriginal() (*configuration.OriginalState, error) {
	return env.originalStore().ReadOriginal(ownership.HasManagedConfig)
}

func (env *Environment) Restore() restore.Service {
	return restore.Service{
		Store: env.Store(), IsRoot: env.IsRoot, CommandExists: env.CommandExists,
		HasManagedConfig: ownership.HasManagedConfig, ReadOriginal: env.ReadOriginal, Autostart: env.Autostart(),
	}
}

func (env *Environment) ruleOptions() rules.Options {
	return rules.Options{
		Store: env.Store(), IsRoot: env.IsRoot, HasManagedConfig: ownership.HasManagedConfig,
	}
}

func (env *Environment) AccessPolicy() rules.Session { return access.Service(env.ruleOptions()) }

func (env *Environment) ForwardPolicy() rules.Session {
	return forward.Service(env.ruleOptions(), env.DockerBridge(), env.DockerMark())
}

func (env *Environment) DockerMark() dockermark.Service {
	return dockermark.Service{
		Store: env.Store(), UnitPath: env.DockerForwardUnitPath,
		IsRoot: env.IsRoot, CommandExists: env.CommandExists, HasManagedConfig: ownership.HasManagedConfig,
	}
}

func (env *Environment) DockerBridge() dockerbridge.Service {
	return dockerbridge.Service{
		Store: env.Store(), IsRoot: env.IsRoot, HasManagedConfig: ownership.HasManagedConfig,
		RequireDocker: env.DockerMark().RequireDocker,
	}
}

func (env *Environment) runFeatureSettings(view *ui.UI) error {
	return (settings.Service{
		CommandExists: env.CommandExists,
		CommandOutput: env.CommandOutput,
		Autostart:     env.Autostart(),
		Ping:          env.Ping(),
		Counters:      env.Counters(),
		Forwarding:    env.IPForwarding(),
		SSH:           env.SSHAccess(),
	}).Run(view)
}

func (env *Environment) Counters() counters.Service {
	return counters.Service{Store: env.Store(), IsRoot: env.IsRoot, Validate: settings.ValidateCounterConfig}
}

func (env *Environment) Ping() ping.Service {
	return ping.Service{
		Store: env.Store(), IsRoot: env.IsRoot, HasManagedConfig: ownership.HasManagedConfig, CommandExists: env.CommandExists,
	}
}

func (env *Environment) Autostart() autostart.Service {
	return autostart.Service{Store: env.Store(), IsRoot: env.IsRoot, CommandExists: env.CommandExists, CommandRun: env.CommandRun}
}

func (env *Environment) IPForwarding() ipforwarding.Service {
	return ipforwarding.Service{
		ProcRoot: env.ForwardingProcRoot, ConfigPath: env.ForwardingConfigPath,
		IsRoot: env.IsRoot, CommandExists: env.CommandExists, CommandOutput: env.CommandOutput,
	}
}

func (env *Environment) SSHAccess() sshaccess.Service {
	return sshaccess.Service{
		Store: env.Store(), IsRoot: env.IsRoot, CommandExists: env.CommandExists, HasManagedConfig: ownership.HasManagedConfig,
	}
}

func (env *Environment) StatusFields(boot autostart.Status) []ui.CardField {
	return (status.Service{
		Store: env.Store(), CommandExists: env.CommandExists, Forwarding: env.IPForwarding(),
	}).Fields(boot)
}
