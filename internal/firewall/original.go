package firewall

import (
	"fmt"
	"strings"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/firewall/forward/dockerbridge"
	rulespkg "snail_tool/internal/firewall/internal/rules"
	"snail_tool/internal/firewall/settings/autostart"
	"snail_tool/internal/firewall/settings/ping"
	"snail_tool/internal/firewall/settings/sshaccess"
)

var preserveOriginal = saveOriginalState

func hasManagedConfig(content string) bool {
	for _, marker := range []string{configuration.ManagedBegin, configuration.ManagedEnd, rulespkg.AccessBegin, rulespkg.AccessEnd, sshaccess.LegacyBegin, sshaccess.LegacyEnd, ping.Begin, ping.End, dockerbridge.Begin, dockerbridge.End} {
		if strings.Contains(content, marker) {
			return true
		}
	}
	return false
}

func originalStore() configuration.Store {
	return configuration.Store{Path: configPath, DataDir: dataDir}
}

func saveOriginalState() error {
	return originalStore().PreserveInitial(hasManagedConfig, inspectOriginalState)
}

func readOriginalState() (*configuration.OriginalState, error) {
	return originalStore().ReadOriginal(hasManagedConfig)
}

func inspectOriginalState() (string, error) {
	if commandExists("nft") {
		live := inspectLiveRules()
		if live.Err != nil {
			return "", fmt.Errorf("无法保存接入前防火墙状态: %w", live.Err)
		}
		if live.SnailLoaded {
			return "", fmt.Errorf("已有 inet snail 表但缺少本工具托管标记，拒绝接管或覆盖原有规则")
		}
	}
	bootState := ""
	if commandExists("systemctl") {
		status := autostart.Service{Store: configuration.Store{CommandOutput: commandOutput}, CommandExists: commandExists}.Inspect()
		if status.Err != nil && status.Label != "服务不可用" {
			return "", status.Err
		}
		if status.Err == nil {
			bootState = status.State
		}
	}
	return bootState, nil
}
