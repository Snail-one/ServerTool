package status

import (
	commonbash "snail_tool/internal/common/bash"
	commonproxy "snail_tool/internal/common/proxy"
	commonups "snail_tool/internal/common/ups"
	commonvim "snail_tool/internal/common/vim"
	containerruntime "snail_tool/internal/container/runtime"
	"snail_tool/internal/environment/golang"
	"snail_tool/internal/ssh/keys"
	"snail_tool/internal/ssh/security"
	"snail_tool/internal/system"
	"strings"
)

type Status struct {
	SSH         bool
	SSHKeys     bool
	SSHSecurity bool
	Vim         bool
	Bash        bool
	Proxy       bool
	UPS         bool
	Runtime     string
	GoVersion   string
	Configured  int
	ConfigTotal int
}

func DetectStatus(account *system.Account) Status {
	sshSecurity := security.IsConfigured()
	result := Status{
		SSHSecurity: sshSecurity,
		UPS:         commonups.IsUPSConfigured(),
		Runtime:     RuntimeSummary(containerruntime.DetectAll()),
		GoVersion:   golang.CurrentVersion(),
		ConfigTotal: 4,
	}
	if account != nil {
		result.SSHKeys = keys.IsConfigured(account)
		result.SSH = result.SSHKeys && result.SSHSecurity
		result.Vim = commonvim.IsVimConfigured(account)
		result.Bash = commonbash.IsBashConfigured(account)
		result.Proxy = commonproxy.IsProxyConfigured(account)
	}
	for _, configured := range []bool{result.Vim, result.Bash, result.Proxy, result.UPS} {
		if configured {
			result.Configured++
		}
	}
	return result
}

func RuntimeSummary(runtimes []containerruntime.Runtime) string {
	if len(runtimes) == 0 {
		return "未安装"
	}
	parts := make([]string, 0, len(runtimes))
	for _, item := range runtimes {
		parts = append(parts, item.Display)
	}
	if len(runtimes) > 1 && runtimes[0].Name == "docker" {
		return strings.Join(parts, "、") + "；容器操作优先 Docker"
	}
	return strings.Join(parts, "、")
}
