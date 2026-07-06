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
	UPS         bool
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
		UPS:         isUPSConfigured(),
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
	return isManagedSSHDConfig(readFileString(customSSHDConfigPath))
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
	_, ok := ConfiguredProxyURL(account)
	return ok
}

func isUPSConfigured() bool {
	nutConf := readFileString(nutConfPath)
	upsConf := readFileString(upsConfPath)
	upsdConf := readFileString(upsdConfPath)
	upsdUsers := readFileString(upsdUsersPath)
	upsmonConf := readFileString(upsmonConfPath)
	upsschedConf := readFileString(upsschedConfPath)
	upsOnBattScriptContent := readFileString(upsOnBattScript)

	return nutStandaloneModeLine.MatchString(nutConf) &&
		hasNUTSection(upsConf, upsMonitorName) &&
		containsLine(upsdConf, upsListenLine) &&
		hasNUTSection(upsdUsers, upsMonitorUser) &&
		strings.Contains(upsdUsers, "upsmon master") &&
		hasUPSMonConfig(upsmonConf) &&
		hasUPSSchedConfig(upsschedConf) &&
		hasUPSOnBattScript(upsOnBattScriptContent) &&
		filePermissionMatches(upsOnBattScriptFile)
}

func hasUPSMonConfig(content string) bool {
	return strings.Contains(content, "MONITOR ups@localhost 1 monuser ") &&
		containsLine(content, "NOTIFYCMD /usr/sbin/upssched") &&
		containsLine(content, "NOTIFYFLAG ONBATT EXEC") &&
		containsLine(content, "NOTIFYFLAG ONLINE EXEC")
}

func hasUPSSchedConfig(content string) bool {
	return containsLine(content, "CMDSCRIPT /usr/local/sbin/ups-onbatt-actions.sh") &&
		containsLine(content, "PIPEFN /run/nut/upssched.pipe") &&
		containsLine(content, "LOCKFN /run/nut/upssched.lock") &&
		containsLine(content, "AT ONBATT * START-TIMER onbatt_shutdown 60") &&
		containsLine(content, "AT ONLINE * CANCEL-TIMER onbatt_shutdown")
}

func hasUPSOnBattScript(content string) bool {
	return strings.Contains(content, "onbatt_shutdown") &&
		strings.Contains(content, "upsmon -c fsd")
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
