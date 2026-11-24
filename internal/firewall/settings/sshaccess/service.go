package sshaccess

import (
	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/ui"
)

type Service struct {
	Store            configuration.Store
	IsRoot           func() bool
	CommandExists    func(string) bool
	HasManagedConfig func(string) bool
	Confirm          func(*ui.UI, string) (bool, error)
}
