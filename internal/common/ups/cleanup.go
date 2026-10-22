package ups

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
)

var (
	cleanupCommandExists = system.CommandExists
	cleanupUnitExists    = system.SystemdUnitExists
	cleanupCommandRun    = system.Run
)

type cleanupAction struct {
	file    nutConfigFile
	data    []byte
	options shared.AtomicWriteOptions
	remove  bool
}

func Cleanup(_ *system.Account) error {
	var actions []cleanupAction
	// Read all current files and originals before stopping services or writing.
	for _, file := range nutConfigFiles {
		current, exists, err := shared.ReadRegularFile(file.path)
		if err != nil {
			return err
		}
		original, err := backupStore.ReadOriginal(file.path)
		if err == nil {
			actions = append(actions, cleanupAction{file: file, data: original.Data, options: shared.AtomicWriteOptions{Mode: original.Mode, ForceMode: true, Owner: original.Owner}})
			continue
		}
		if !os.IsNotExist(err) {
			return err
		}
		if !exists {
			continue
		}
		matches, err := backupStore.MatchesManaged(file.path, current)
		if err != nil {
			return err
		}
		if !matches && (cleanupNUTContent(filepath.Base(file.path), string(current)) != string(current) || hasUPSOnBattScript(string(current))) {
			return fmt.Errorf("UPS 配置缺少首次备份和有效写入记录，已保留，请手动检查: %s", file.path)
		}
		if filepath.Base(file.path) == filepath.Base(upsOnBattScript) {
			if strings.TrimSpace(string(current)) == strings.TrimSpace(applyUPSOnBattScript("")) {
				actions = append(actions, cleanupAction{file: file, remove: true})
			} else if hasUPSOnBattScript(string(current)) {
				return fmt.Errorf("UPS 断电脚本已被修改且没有首次备份，已保留，请手动检查: %s", file.path)
			}
			continue
		}
		cleaned := cleanupNUTContent(filepath.Base(file.path), string(current))
		if cleaned != string(current) {
			actions = append(actions, cleanupAction{file: file, data: []byte(cleaned), options: shared.AtomicWriteOptions{Mode: file.mode}})
		}
	}
	if len(actions) == 0 {
		log.Info("未发现可清理的 UPS 配置")
		return nil
	}
	if cleanupCommandExists("systemctl") {
		for _, unit := range []string{"nut-monitor.service", "nut-server.service", "nut-driver@ups.service"} {
			if cleanupUnitExists(unit) {
				if err := cleanupCommandRun("systemctl", "disable", "--now", unit); err != nil {
					return fmt.Errorf("停止并禁用 UPS 服务失败，尚未修改配置: %w", err)
				}
			}
		}
	}
	for _, action := range actions {
		if action.remove {
			if err := shared.RemoveRegularFile(action.file.path); err != nil {
				return err
			}
		} else if err := shared.AtomicWriteFile(action.file.path, action.data, action.options); err != nil {
			return err
		}
		if err := backupStore.ForgetManaged(action.file.path); err != nil {
			return err
		}
		log.Info("已清理 UPS 配置：", action.file.path)
	}
	return nil
}

func cleanupNUTContent(name, content string) string {
	switch name {
	case "nut.conf":
		return nutStandaloneModeLine.ReplaceAllString(content, "MODE=none")
	case "ups.conf", "upsd.users":
		section := upsMonitorName
		if name == "upsd.users" {
			section = upsMonitorUser
		}
		if !hasNUTSection(content, section) {
			return content
		}
		return shared.NormalizeCleanedContent(replaceNUTSection(content, section, ""))
	}
	var kept []string
	for _, line := range strings.SplitAfter(content, "\n") {
		trimmed := strings.TrimSpace(line)
		remove := false
		switch name {
		case "upsd.conf":
			remove = trimmed == upsListenLine
		case "upsmon.conf":
			fields := strings.Fields(trimmed)
			remove = len(fields) >= 4 && fields[0] == "MONITOR" && fields[1] == "ups@localhost" && fields[3] == upsMonitorUser
			for _, own := range []string{"NOTIFYCMD /usr/sbin/upssched", "NOTIFYFLAG ONBATT EXEC", "NOTIFYFLAG ONLINE EXEC"} {
				remove = remove || trimmed == own
			}
		case "upssched.conf":
			for _, own := range []string{
				"CMDSCRIPT /usr/local/sbin/ups-onbatt-actions.sh", "PIPEFN /run/nut/upssched.pipe", "LOCKFN /run/nut/upssched.lock",
				"AT ONBATT * START-TIMER onbatt_shutdown 60", "AT ONLINE * CANCEL-TIMER onbatt_shutdown",
			} {
				remove = remove || trimmed == own
			}
		}
		if !remove {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "")
}
