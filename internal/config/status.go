package config

import (
	"os"
	"path/filepath"
	"strings"

	"snail_tool/internal/system"
)

type Status struct {
	SSH         bool
	SSHKeys     bool
	SSHSecurity bool
	Vim         bool
	Bash        bool
	Proxy       bool
}

func DetectStatus(account *system.Account) Status {
	sshKeys := isSSHKeysConfigured(account)
	sshSecurity := isSSHSecurityConfigured()
	return Status{
		SSH:         sshKeys && sshSecurity,
		SSHKeys:     sshKeys,
		SSHSecurity: sshSecurity,
		Vim:         isVimConfigured(account),
		Bash:        isBashConfigured(account),
		Proxy:       isProxyConfigured(account),
	}
}

func isSSHConfigured(account *system.Account) bool {
	return isSSHKeysConfigured(account) && isSSHSecurityConfigured()
}

func isSSHKeysConfigured(account *system.Account) bool {
	authKeys := filepath.Join(account.Home, ".ssh", "authorized_keys")
	return fileContainsNonEmptyContent(authKeys)
}

func isSSHSecurityConfigured() bool {
	return fileContains(customSSHDConfigPath, managedSSHDConfigHeader)
}

func isVimConfigured(account *system.Account) bool {
	vimrc := filepath.Join(account.Home, ".vimrc")
	return strings.TrimSpace(readFileString(vimrc)) == strings.TrimSpace(vimrcContent)
}

func isBashConfigured(account *system.Account) bool {
	bashrc := filepath.Join(account.Home, ".bashrc")
	content := readFileString(bashrc)
	return strings.Contains(content, bashAliasBegin) &&
		strings.Contains(content, bashAliasBlock) &&
		strings.Contains(content, bashAliasEnd)
}

func isProxyConfigured(account *system.Account) bool {
	_, ok := CurrentProxyURL(account)
	return ok
}

func fileContains(path, wanted string) bool {
	return strings.Contains(readFileString(path), wanted)
}

func fileContainsNonEmptyContent(path string) bool {
	return strings.TrimSpace(readFileString(path)) != ""
}

func readFileString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
