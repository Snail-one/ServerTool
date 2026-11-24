package ups

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
)

func writeUPSConfigFiles(device upsDeviceConfig, password string) error {
	if err := os.MkdirAll(nutConfigDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(upsOnBattScript), 0755); err != nil {
		return err
	}

	writes := []struct {
		file      nutConfigFile
		transform func(string) string
	}{
		{file: nutConfFile, transform: applyNUTStandaloneMode},
		{file: upsConfFile, transform: func(content string) string {
			return replaceNUTSection(content, upsMonitorName, buildUPSDeviceBlock(device))
		}},
		{file: upsdConfFile, transform: applyUPSDListenConfig},
		{file: upsdUsersFile, transform: func(content string) string {
			return replaceNUTSection(content, upsMonitorUser, buildUPSDUserBlock(password))
		}},
		{file: upsmonConfFile, transform: func(content string) string {
			return applyUPSMonConfig(content, password)
		}},
		{file: upsschedConfFile, transform: applyUPSSchedConfig},
		{file: upsOnBattScriptFile, transform: applyUPSOnBattScript},
	}

	for _, write := range writes {
		if err := writeNUTConfigFile(write.file, write.transform); err != nil {
			return err
		}
	}
	warnIfNUTConfigPermissionsMismatch()
	return nil
}

func writeNUTConfigFile(file nutConfigFile, transform func(string) string) error {
	path := file.path
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) && requiresExistingNUTConfigFile(file) {
			return fmt.Errorf("NUT 配置文件不存在，请先安装 NUT 并确认默认配置文件存在: %s", path)
		}
		if !os.IsNotExist(err) {
			return err
		}
	}

	content := string(data)
	updated := transform(content)
	if content == updated {
		if err := applyManagedFileMetadataIfNeeded(file); err != nil {
			return err
		}
		log.Info("NUT 配置已存在：", path)
		return nil
	}

	if err == nil {
		backup, created, backupErr := backupNUTConfigFile(path)
		if backupErr != nil {
			return backupErr
		}
		if created {
			log.Info("已创建原始配置备份：", backup)
		} else {
			log.Info("已保留原始配置备份：", backup)
		}
	}

	options := shared.AtomicWriteOptions{Mode: file.mode}
	if shouldEnforceFileMetadata(file) {
		options.ForceMode = true
		if uid, gid, ownerErr := lookupFileOwner(file.owner); ownerErr == nil {
			options.Owner = &shared.FileOwner{UID: uid, GID: gid}
		} else {
			return ownerErr
		}
	}
	if err := backupStore.RememberManaged(path, []byte(updated)); err != nil {
		return fmt.Errorf("记录 UPS 配置归属失败，未写入配置: %w", err)
	}
	if err := shared.AtomicWriteFile(path, []byte(updated), options); err != nil {
		return err
	}
	if err := applyManagedFileMetadataIfNeeded(file); err != nil {
		return err
	}
	log.Info("已写入 NUT 配置：", path)
	return nil
}

func requiresExistingNUTConfigFile(file nutConfigFile) bool {
	return file.owner == rootNutOwner && !shouldEnforceFileMetadata(file)
}

func applyNUTStandaloneMode(content string) string {
	if nutModeLine.MatchString(content) {
		return nutModeLine.ReplaceAllString(content, "${1}standalone")
	}
	return shared.AppendBlock(content, "MODE=standalone\n")
}

func buildUPSDeviceBlock(device upsDeviceConfig) string {
	return fmt.Sprintf(`[ups]
driver = usbhid-ups
port = auto
vendorid = %s
productid = %s
desc = "%s"
`, device.VendorID, device.ProductID, device.Desc)
}

func applyUPSDListenConfig(content string) string {
	if shared.ContainsLine(content, upsListenLine) {
		return content
	}
	return shared.AppendBlock(content, upsListenLine+"\n")
}

func buildUPSDUserBlock(password string) string {
	return fmt.Sprintf(`[monuser]
  password = %s
  upsmon master
`, password)
}

func applyUPSMonConfig(content, password string) string {
	cleaned := removeUPSMonGeneratedLines(content)
	block := fmt.Sprintf(`MONITOR ups@localhost 1 monuser %s master
NOTIFYCMD /usr/sbin/upssched
NOTIFYFLAG ONBATT EXEC
NOTIFYFLAG ONLINE EXEC
`, password)
	return shared.AppendBlock(cleaned, block)
}

func applyUPSSchedConfig(content string) string {
	cleaned := removeUPSSchedGeneratedLines(content)
	block := `CMDSCRIPT /usr/local/sbin/ups-onbatt-actions.sh
PIPEFN /run/nut/upssched.pipe
LOCKFN /run/nut/upssched.lock

AT ONBATT * START-TIMER onbatt_shutdown 60
AT ONLINE * CANCEL-TIMER onbatt_shutdown
`
	return shared.AppendBlock(cleaned, block)
}

func applyUPSOnBattScript(string) string {
	return `#!/bin/sh

case "$1" in
  onbatt_shutdown)
    logger -t nut "ONBATT timer expired (60s), triggering FSD"
    exec /usr/sbin/upsmon -c fsd
    ;;
  *)
    logger -t nut "upssched called with unknown event: $1"
    exit 0
    ;;
esac
`
}

func removeUPSMonGeneratedLines(content string) string {
	lines := strings.SplitAfter(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimRight(line, "\r\n"))
		if shouldRemoveUPSMonLine(trimmed) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "")
}

func shouldRemoveUPSMonLine(trimmed string) bool {
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return false
	}

	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return false
	}

	switch fields[0] {
	case "MONITOR":
		return len(fields) > 1 && fields[1] == "ups@localhost"
	case "NOTIFYCMD":
		return true
	case "NOTIFYFLAG":
		return len(fields) > 1 && (fields[1] == "ONBATT" || fields[1] == "ONLINE")
	default:
		return false
	}
}

func removeUPSSchedGeneratedLines(content string) string {
	lines := strings.SplitAfter(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimRight(line, "\r\n"))
		if shouldRemoveUPSSchedLine(trimmed) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "")
}

func shouldRemoveUPSSchedLine(trimmed string) bool {
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return false
	}

	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return false
	}

	switch fields[0] {
	case "CMDSCRIPT", "PIPEFN", "LOCKFN":
		return true
	case "AT":
		return len(fields) > 1 && (fields[1] == "ONBATT" || fields[1] == "ONLINE")
	default:
		return false
	}
}

func replaceNUTSection(content, sectionName, block string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	kept := make([]string, 0, len(lines))
	skip := false

	for _, line := range lines {
		if name, ok := nutSectionName(line); ok {
			skip = strings.EqualFold(name, sectionName)
			if skip {
				continue
			}
		}
		if skip {
			continue
		}
		kept = append(kept, line)
	}

	cleaned := strings.TrimRight(strings.Join(kept, "\n"), "\n")
	return shared.AppendBlock(cleaned, strings.TrimRight(block, "\n")+"\n")
}

func nutSectionName(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || !strings.HasPrefix(trimmed, "[") {
		return "", false
	}

	end := strings.Index(trimmed, "]")
	if end <= 1 {
		return "", false
	}

	tail := strings.TrimSpace(trimmed[end+1:])
	if tail != "" && !strings.HasPrefix(tail, "#") {
		return "", false
	}

	return strings.TrimSpace(trimmed[1:end]), true
}

func hasNUTSection(content, sectionName string) bool {
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		name, ok := nutSectionName(line)
		if ok && strings.EqualFold(name, sectionName) {
			return true
		}
	}
	return false
}

func IsUPSConfigured() bool {
	nutConf := shared.ReadFileString(nutConfPath)
	upsConf := shared.ReadFileString(upsConfPath)
	upsdConf := shared.ReadFileString(upsdConfPath)
	upsdUsers := shared.ReadFileString(upsdUsersPath)
	upsmonConf := shared.ReadFileString(upsmonConfPath)
	upsschedConf := shared.ReadFileString(upsschedConfPath)
	upsOnBattScriptContent := shared.ReadFileString(upsOnBattScript)

	return nutStandaloneModeLine.MatchString(nutConf) &&
		hasNUTSection(upsConf, upsMonitorName) &&
		shared.ContainsLine(upsdConf, upsListenLine) &&
		hasNUTSection(upsdUsers, upsMonitorUser) &&
		strings.Contains(upsdUsers, "upsmon master") &&
		hasUPSMonConfig(upsmonConf) &&
		hasUPSSchedConfig(upsschedConf) &&
		hasUPSOnBattScript(upsOnBattScriptContent) &&
		filePermissionMatches(upsOnBattScriptFile)
}

func hasUPSMonConfig(content string) bool {
	return strings.Contains(content, "MONITOR ups@localhost 1 monuser ") &&
		shared.ContainsLine(content, "NOTIFYCMD /usr/sbin/upssched") &&
		shared.ContainsLine(content, "NOTIFYFLAG ONBATT EXEC") &&
		shared.ContainsLine(content, "NOTIFYFLAG ONLINE EXEC")
}

func hasUPSSchedConfig(content string) bool {
	return shared.ContainsLine(content, "CMDSCRIPT /usr/local/sbin/ups-onbatt-actions.sh") &&
		shared.ContainsLine(content, "PIPEFN /run/nut/upssched.pipe") &&
		shared.ContainsLine(content, "LOCKFN /run/nut/upssched.lock") &&
		shared.ContainsLine(content, "AT ONBATT * START-TIMER onbatt_shutdown 60") &&
		shared.ContainsLine(content, "AT ONLINE * CANCEL-TIMER onbatt_shutdown")
}

func hasUPSOnBattScript(content string) bool {
	return strings.Contains(content, "onbatt_shutdown") &&
		strings.Contains(content, "upsmon -c fsd")
}

func randomPassword(length int) (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	var builder strings.Builder
	builder.Grow(length)
	for i := 0; i < length; i++ {
		index, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		builder.WriteByte(alphabet[index.Int64()])
	}
	return builder.String(), nil
}
