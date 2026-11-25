package sshaccess

import (
	"snail_tool/internal/firewall/configuration"
)

type Service struct {
	Store            configuration.Store
	IsRoot           func() bool
	CommandExists    func(string) bool
	HasManagedConfig func(string) bool
}
