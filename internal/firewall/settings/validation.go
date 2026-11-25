package settings

import (
	"snail_tool/internal/firewall/internal/rules"
	"snail_tool/internal/firewall/settings/ping"
)

func ValidateCounterConfig(content string) error {
	if _, err := rules.ManagedRules(content); err != nil {
		return err
	}
	_, _, err := ping.Read(content)
	return err
}
