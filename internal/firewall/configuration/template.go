package configuration

import (
	_ "embed"
	"strings"
)

//go:embed nftables.conf
var configTemplate string

func RenderTemplate(sshText string, selection PortSelection) (content string, clearPublic, clearPrivate bool) {
	publicPorts, publicDescription := scopePortText(selection.PublicSSH, sshText, selection.PublicWeb)
	privatePorts, privateDescription := scopePortText(selection.PrivateSSH, sshText, selection.PrivateWeb)
	content = strings.NewReplacer(
		"__SSH_PORTS__", sshText,
		"__PUBLIC_PORTS__", publicPorts,
		"__PRIVATE_PORTS__", privatePorts,
		"__PUBLIC_PORT_DESCRIPTION__", publicDescription,
		"__PRIVATE_PORT_DESCRIPTION__", privateDescription,
	).Replace(configTemplate)
	return content, publicPorts == "", privatePorts == ""
}

func scopePortText(openSSH bool, sshText string, openWeb bool) (string, string) {
	var ports []string
	if openSSH {
		ports = append(ports, sshText)
	}
	if openWeb {
		ports = append(ports, "80, 443")
	}
	switch {
	case openSSH && openWeb:
		return strings.Join(ports, ", "), "当前开放 SSH 端口及 80、443"
	case openSSH:
		return strings.Join(ports, ", "), "当前仅开放 SSH 端口"
	case openWeb:
		return strings.Join(ports, ", "), "当前仅开放 80、443"
	default:
		return "", "当前未开放端口"
	}
}

const ManagedBegin = "# ===== BEGIN SNAIL NFTABLES CONFIG ====="
const ManagedEnd = "# ===== END SNAIL NFTABLES CONFIG ====="
