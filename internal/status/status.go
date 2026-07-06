package status

import (
	commonbash "snail_tool/internal/common/bash"
	commonproxy "snail_tool/internal/common/proxy"
	commonups "snail_tool/internal/common/ups"
	commonvim "snail_tool/internal/common/vim"
	"snail_tool/internal/ssh/keys"
	"snail_tool/internal/ssh/security"
	"snail_tool/internal/system"
)

type Status struct {
	SSH         bool
	SSHKeys     bool
	SSHSecurity bool
	Vim         bool
	Bash        bool
	Proxy       bool
	UPS         bool
}

func DetectStatus(account *system.Account) Status {
	sshKeys := keys.IsConfigured(account)
	sshSecurity := security.IsConfigured()
	return Status{
		SSH:         sshKeys && sshSecurity,
		SSHKeys:     sshKeys,
		SSHSecurity: sshSecurity,
		Vim:         commonvim.IsVimConfigured(account),
		Bash:        commonbash.IsBashConfigured(account),
		Proxy:       commonproxy.IsProxyConfigured(account),
		UPS:         commonups.IsUPSConfigured(),
	}
}
