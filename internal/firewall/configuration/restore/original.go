package restore

import (
	"fmt"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/firewall/configuration/ownership"
	"snail_tool/internal/firewall/loadedrules"
	"snail_tool/internal/firewall/settings/autostart"
)

func PreserveInitial(store configuration.Store, commandExists func(string) bool) error {
	return store.PreserveInitial(ownership.HasManagedConfig, func() (string, error) {
		return inspectOriginalState(commandExists, store.CommandOutput)
	})
}

func inspectOriginalState(commandExists func(string) bool, commandOutput func(string, ...string) (string, error)) (string, error) {
	if commandExists("nft") {
		live := loadedrules.Inspect(commandOutput)
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
