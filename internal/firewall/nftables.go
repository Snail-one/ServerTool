package firewall

import (
	"snail_tool/internal/firewall/nftables"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

var (
	commandExists = system.CommandExists
	commandRun    = system.Run
	commandOutput = system.Output
	isRoot        = system.IsRoot
)

func ensureNftables(view *ui.UI) (bool, error) {
	return nftables.Ensure(view, commandExists, commandRun)
}
