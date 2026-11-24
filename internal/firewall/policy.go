package firewall

import (
	"snail_tool/internal/firewall/access"
	"snail_tool/internal/firewall/forward"
	"snail_tool/internal/firewall/internal/rules"
	"snail_tool/internal/ui"
)

func ruleOptions() rules.Options {
	return rules.Options{
		Store: configStore(), IsRoot: isRoot, HasManagedConfig: hasManagedConfig,
	}
}

func accessPolicy() rules.Session { return access.Service(ruleOptions()) }
func forwardPolicy() rules.Session {
	return forward.Service(ruleOptions(), dockerBridgeService(), dockerMarkService())
}

func runAccessRuleManager(view *ui.UI) error  { return accessPolicy().Run(view) }
func runForwardRuleManager(view *ui.UI) error { return forwardPolicy().Run(view) }
