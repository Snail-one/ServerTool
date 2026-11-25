package status

import (
	"strings"

	commonbash "snail_tool/internal/configuration/bash"
	commonproxy "snail_tool/internal/configuration/proxy"
	commonvim "snail_tool/internal/configuration/vim"
	"snail_tool/internal/container"
	"snail_tool/internal/ssh/keys"
	"snail_tool/internal/ssh/security"
	"snail_tool/internal/system"
	"snail_tool/internal/tools/golang"
	"snail_tool/internal/tools/ups"
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

// DetectStatusWithProgress reports the current probe without changing how
// configuration status is determined. A nil step keeps detection silent.
func DetectStatusWithProgress(account *system.Account, step func(string, func())) Status {
	if step == nil {
		step = func(_ string, detect func()) { detect() }
	}
	result := Status{ConfigTotal: 3}
	step("SSH 安全配置", func() { result.SSHSecurity = security.IsConfigured() })
	step("UPS 配置", func() { result.UPS = ups.IsUPSConfigured() })
	result.Runtime = RuntimeSummary(container.DetectAllWithProgress(step))
	step("Go 当前版本", func() { result.GoVersion = golang.CurrentVersion() })
	if account != nil {
		step("SSH 公钥", func() { result.SSHKeys = keys.IsConfigured(account) })
		result.SSH = result.SSHKeys && result.SSHSecurity
		step("Vim 配置", func() { result.Vim = commonvim.IsVimConfigured(account) })
		step("Bash 配置", func() { result.Bash = commonbash.IsBashConfigured(account) })
		step("代理配置", func() { result.Proxy = commonproxy.IsProxyConfigured(account) })
	}
	for _, configured := range []bool{result.Vim, result.Bash, result.Proxy} {
		if configured {
			result.Configured++
		}
	}
	return result
}

func RuntimeSummary(runtimes []container.Runtime) string {
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
