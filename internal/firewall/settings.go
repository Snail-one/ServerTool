package firewall

import (
	rulespkg "snail_tool/internal/firewall/internal/rules"
	"snail_tool/internal/firewall/loadedrules"
	"snail_tool/internal/firewall/settings"
	"snail_tool/internal/firewall/settings/autostart"
	"snail_tool/internal/firewall/settings/counters"
	"snail_tool/internal/firewall/settings/ipforwarding"
	"snail_tool/internal/firewall/settings/ping"
	"snail_tool/internal/firewall/settings/sshaccess"
	"snail_tool/internal/ui"
)

func confirmFeatureChange(view *ui.UI, question string) (bool, error) {
	return ui.ConfirmMenuChange(view, question, "常用功能设置")
}

func runFeatureSettings(view *ui.UI) error {
	return settings.Run(view, settings.Actions{
		Inspect:    inspectFeatureSettings,
		Autostart:  autostartService().Toggle,
		Ping:       configurePing,
		Counters:   configurePacketCounters,
		Forwarding: ipForwardingService().Run,
		SSH:        sshAccessService().Run,
	})
}

func inspectFeatureSettings() settings.Snapshot {
	autostart := autostartService().Inspect()
	installed := commandExists("nft")
	live := loadedrules.Snapshot{}
	if installed {
		live = inspectLiveRules()
	}
	autostartLabel := ui.StateText(autostart.Label, autostart.Enabled || autostart.State == "enabled-runtime")
	fields := []ui.CardField{{Label: "开机自启", Value: autostartLabel}}
	fields = append(fields, echoStateFields(installed, live)...)
	fields = append(fields, packetCounterStateField(installed, live))
	fields = append(fields, ipForwardingService().StatusField())
	fields = append(fields, sshAccessService().StateField())
	return settings.Snapshot{
		Fields: fields, AutostartUnavailable: autostart.Err != nil, AutostartEnabled: autostart.Enabled,
	}
}

func packetCounterStateField(installed bool, live loadedrules.Snapshot) ui.CardField {
	return counters.StateField(installed, live.InputActive, live.JSON, live.Err)
}

func echoStateFields(installed bool, live loadedrules.Snapshot) []ui.CardField {
	return ping.StateFields(installed, live.InputActive, live.JSON, live.Err)
}

func validateCounterConfig(content string) error {
	if _, err := rulespkg.ManagedRules(content); err != nil {
		return err
	}
	_, _, err := ping.Read(content)
	return err
}

func configurePacketCounters(view *ui.UI) error {
	return (counters.Service{Store: configStore(), IsRoot: isRoot, Validate: validateCounterConfig}).Run(view)
}

func configurePing(view *ui.UI) error {
	return (ping.Service{
		Store: configStore(), IsRoot: isRoot, HasManagedConfig: hasManagedConfig, CommandExists: commandExists,
		InspectLive: func() (string, bool, error) {
			live := inspectLiveRules()
			return live.JSON, live.InputActive, live.Err
		},
	}).Run(view)
}

func autostartService() autostart.Service {
	return autostart.Service{Store: configStore(), IsRoot: isRoot, CommandExists: commandExists, CommandRun: commandRun}
}

var (
	forwardingProcRoot   = ipforwarding.DefaultProcRoot
	forwardingConfigPath = ipforwarding.DefaultConfigPath
)

func ipForwardingService() ipforwarding.Service {
	return ipforwarding.Service{
		ProcRoot: forwardingProcRoot, ConfigPath: forwardingConfigPath,
		IsRoot: isRoot, CommandExists: commandExists, CommandOutput: commandOutput,
	}
}

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
