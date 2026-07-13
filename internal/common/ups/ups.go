package ups

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

const (
	nutConfigDir      = "/etc/nut"
	nutConfPath       = "/etc/nut/nut.conf"
	upsConfPath       = "/etc/nut/ups.conf"
	upsdConfPath      = "/etc/nut/upsd.conf"
	upsdUsersPath     = "/etc/nut/upsd.users"
	upsmonConfPath    = "/etc/nut/upsmon.conf"
	upsschedConfPath  = "/etc/nut/upssched.conf"
	upsOnBattScript   = "/usr/local/sbin/ups-onbatt-actions.sh"
	upsListenLine     = "LISTEN 127.0.0.1 3493"
	upsMonitorName    = "ups"
	upsMonitorUser    = "monuser"
	upsPasswordLength = 24
)

type fileOwner struct {
	user  string
	group string
}

type nutConfigFile struct {
	path  string
	mode  os.FileMode
	owner fileOwner
}

var (
	nutModeLine           = regexp.MustCompile(`(?m)^([ \t]*MODE[ \t]*=[ \t]*).*$`)
	nutStandaloneModeLine = regexp.MustCompile(`(?m)^[ \t]*MODE[ \t]*=[ \t]*standalone[ \t]*(?:#.*)?$`)
	usbIDLine             = regexp.MustCompile(`(?i)^[0-9a-f]{4}$`)
	rootNutOwner          = fileOwner{user: "root", group: "nut"}
	rootOwner             = fileOwner{user: "root", group: "root"}
	nutOwner              = fileOwner{user: "nut", group: "nut"}
	nutConfFile           = nutConfigFile{path: nutConfPath, mode: 0640, owner: rootNutOwner}
	upsConfFile           = nutConfigFile{path: upsConfPath, mode: 0640, owner: rootNutOwner}
	upsdConfFile          = nutConfigFile{path: upsdConfPath, mode: 0640, owner: rootNutOwner}
	upsdUsersFile         = nutConfigFile{path: upsdUsersPath, mode: 0640, owner: rootNutOwner}
	upsmonConfFile        = nutConfigFile{path: upsmonConfPath, mode: 0640, owner: rootNutOwner}
	upsschedConfFile      = nutConfigFile{path: upsschedConfPath, mode: 0640, owner: rootNutOwner}
	upsOnBattScriptFile   = nutConfigFile{path: upsOnBattScript, mode: 0700, owner: nutOwner}
	nutConfigFiles        = []nutConfigFile{
		nutConfFile,
		upsConfFile,
		upsdConfFile,
		upsdUsersFile,
		upsmonConfFile,
		upsschedConfFile,
		upsOnBattScriptFile,
	}
)

type upsDeviceConfig struct {
	VendorID  string
	ProductID string
	Desc      string
}

func Run(view *ui.UI) error {
	return ConfigureUPS(view)
}

func ConfigureUPS(view *ui.UI) error {
	log.Info("配置 UPS (NUT)...")

	if !system.IsRoot() {
		return fmt.Errorf("UPS 配置需要 root 权限，请使用 sudo 运行 snail_tool")
	}

	for {
		ui.MenuTitle("系统与用户配置", "UPS（NUT）")
		fmt.Println("1) 配置或更新 UPS")
		fmt.Println("2) 恢复首次备份（官方默认配置）")
		fmt.Println("3) 删除 UPS 配置备份")
		fmt.Println("0/q) 返回")
		fmt.Println()

		choice, err := view.Ask("输入选项: ")
		if err != nil {
			return err
		}
		fmt.Println()

		if shared.IsReturnChoice(choice) {
			return shared.ErrReturnToMenu
		}
		switch strings.ToLower(choice) {
		case "1":
			return configureUPS(view)
		case "2":
			return restoreUPSBackup(view)
		case "3":
			return deleteUPSBackups(view)
		default:
			fmt.Println("无效选项，请重新输入")
			fmt.Println()
		}
	}
}

func configureUPS(view *ui.UI) error {
	installed, err := ensureNUTInstalled(view)
	if err != nil {
		return err
	}
	if !installed {
		return nil
	}

	printLSUSBDevices()

	device, err := askUPSDeviceConfig(view)
	if err != nil {
		return err
	}

	password, err := randomPassword(upsPasswordLength)
	if err != nil {
		return err
	}

	if err := writeUPSConfigFiles(device, password); err != nil {
		return err
	}

	if err := activateNUTServices(); err != nil {
		return err
	}

	if err := verifyUPSCommunication(); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("UPS 配置完成")
	return nil
}

func ensureNUTInstalled(view *ui.UI) (bool, error) {
	if isNUTInstalled() {
		return true, nil
	}

	log.Warn("未检测到 NUT 服务端组件")
	fmt.Println("请选择安装方式：")
	fmt.Println("1) 手动安装并返回")
	fmt.Println("2) 自动安装 NUT")
	fmt.Println("0/q) 取消")
	fmt.Println()

	for {
		choice, err := view.Ask("输入选项: ")
		if err != nil {
			return false, err
		}
		fmt.Println()
		if shared.IsReturnChoice(choice) {
			return false, nil
		}

		switch strings.ToLower(choice) {
		case "1":
			printNUTManualInstallHint()
			return false, nil
		case "2":
			if err := installNUT(); err != nil {
				return false, err
			}
			if !isNUTInstalled() {
				return false, fmt.Errorf("NUT 自动安装后仍未检测到服务端组件，请检查安装输出")
			}
			return true, nil
		default:
			fmt.Println("无效选项，请重新输入")
		}
	}
}

func isNUTInstalled() bool {
	if system.DirExists(nutConfigDir) && commandAvailable("upsc", "/usr/bin/upsc") && nutServerComponentAvailable() {
		return true
	}
	return nutPackageInstalled()
}

func nutServerComponentAvailable() bool {
	return commandAvailable("upsd", "/usr/sbin/upsd", "/sbin/upsd") ||
		commandAvailable("usbhid-ups", "/lib/nut/usbhid-ups", "/usr/lib/nut/usbhid-ups", "/usr/libexec/nut/usbhid-ups")
}

func nutPackageInstalled() bool {
	switch {
	case system.CommandExists("dpkg-query"):
		out, err := system.Output("dpkg-query", "-W", "-f=${Status}", "nut")
		return err == nil && strings.Contains(out, "install ok installed")
	case system.CommandExists("rpm"):
		_, err := system.Output("rpm", "-q", "nut")
		return err == nil
	case system.CommandExists("pacman"):
		_, err := system.Output("pacman", "-Q", "nut")
		return err == nil
	default:
		return false
	}
}

func commandAvailable(name string, fallbackPaths ...string) bool {
	if system.CommandExists(name) {
		return true
	}
	for _, path := range fallbackPaths {
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

func printNUTManualInstallHint() {
	fmt.Println("请手动安装 NUT 后重新进入 UPS 配置。")
	fmt.Println()
	fmt.Println("常见命令：")
	switch {
	case system.CommandExists("apt-get"):
		fmt.Println("sudo apt update")
		fmt.Println("sudo apt install -y nut")
	case system.CommandExists("dnf"):
		fmt.Println("sudo dnf install -y nut")
	case system.CommandExists("yum"):
		fmt.Println("sudo yum install -y nut")
	case system.CommandExists("pacman"):
		fmt.Println("sudo pacman -Sy --noconfirm nut")
	case system.CommandExists("zypper"):
		fmt.Println("sudo zypper --non-interactive install nut")
	default:
		fmt.Println("请使用当前系统的包管理器安装 nut")
	}
}

func installNUT() error {
	log.Info("开始自动安装 NUT...")

	switch {
	case system.CommandExists("apt-get"):
		if err := system.Run("apt-get", "update"); err != nil {
			return fmt.Errorf("apt-get update 失败: %w", err)
		}
		return system.Run("apt-get", "install", "-y", "nut")
	case system.CommandExists("dnf"):
		return system.Run("dnf", "install", "-y", "nut")
	case system.CommandExists("yum"):
		return system.Run("yum", "install", "-y", "nut")
	case system.CommandExists("pacman"):
		return system.Run("pacman", "-Sy", "--noconfirm", "nut")
	case system.CommandExists("zypper"):
		return system.Run("zypper", "--non-interactive", "install", "nut")
	default:
		return fmt.Errorf("未识别支持的包管理器，请手动安装 NUT")
	}
}

func printLSUSBDevices() {
	fmt.Println("当前 USB 设备列表：")
	fmt.Println("----------")
	if !system.CommandExists("lsusb") {
		log.Warn("未找到 lsusb 命令，已跳过 USB 设备列表")
		fmt.Println("----------")
		fmt.Println()
		return
	}

	if err := system.Run("lsusb"); err != nil {
		log.Warn("执行 lsusb 失败：", err)
	}
	fmt.Println("----------")
	fmt.Println()
}

func askUPSDeviceConfig(view *ui.UI) (upsDeviceConfig, error) {
	vendorID, err := askUSBID(view, "请输入 vendorid（例如 0463）: ")
	if err != nil {
		return upsDeviceConfig{}, err
	}

	productID, err := askUSBID(view, "请输入 productid（例如 ffff）: ")
	if err != nil {
		return upsDeviceConfig{}, err
	}

	desc, err := view.Ask("请输入 desc（例如 SANTAK TG-BOX 850 USB UPS）: ")
	if err != nil {
		return upsDeviceConfig{}, err
	}
	desc = normalizeUPSDescription(desc)
	if desc == "" {
		return upsDeviceConfig{}, fmt.Errorf("desc 不能为空")
	}

	return upsDeviceConfig{
		VendorID:  strings.ToLower(vendorID),
		ProductID: strings.ToLower(productID),
		Desc:      desc,
	}, nil
}

func askUSBID(view *ui.UI, prompt string) (string, error) {
	value, err := view.Ask(prompt)
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	if !usbIDLine.MatchString(value) {
		return "", fmt.Errorf("USB ID 必须是 4 位十六进制字符")
	}
	return value, nil
}

func normalizeUPSDescription(desc string) string {
	desc = strings.TrimSpace(desc)
	desc = strings.Trim(desc, `"'`)
	desc = strings.ReplaceAll(desc, `\`, `\\`)
	desc = strings.ReplaceAll(desc, `"`, `\"`)
	return desc
}

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

func applyManagedFileMetadataIfNeeded(file nutConfigFile) error {
	if !shouldEnforceFileMetadata(file) {
		return nil
	}
	return applyManagedFileMetadata(file)
}

func shouldEnforceFileMetadata(file nutConfigFile) bool {
	return file.path == upsOnBattScript
}

func applyManagedFileMetadata(file nutConfigFile) error {
	if file.mode != 0 {
		if err := os.Chmod(file.path, file.mode); err != nil {
			return err
		}
	}
	if file.owner.user == "" && file.owner.group == "" {
		return nil
	}

	uid, gid, err := lookupFileOwner(file.owner)
	if err != nil {
		return err
	}
	if err := os.Chown(file.path, uid, gid); err != nil {
		return err
	}
	return nil
}

func warnIfNUTConfigPermissionsMismatch() {
	files := []nutConfigFile{
		nutConfFile,
		upsConfFile,
		upsdConfFile,
		upsdUsersFile,
		upsmonConfFile,
		upsschedConfFile,
	}

	for _, file := range files {
		if !filePermissionMatches(file) {
			printNUTConfigFilePermissions(files)
			return
		}
	}
}

func filePermissionMatches(file nutConfigFile) bool {
	info, err := os.Stat(file.path)
	if err != nil || info.IsDir() {
		return false
	}
	if info.Mode().Perm() != file.mode {
		return false
	}
	owner, group := fileOwnerNames(info)
	return owner == file.owner.user && group == file.owner.group
}

func printNUTConfigFilePermissions(files []nutConfigFile) {
	log.Warn("/etc/nut 配置文件权限不是预期的 0640 root:nut，当前权限如下：")
	for _, file := range files {
		permission, err := describeFilePermission(file.path)
		if err != nil {
			fmt.Printf("- %s: %v\n", file.path, err)
			continue
		}
		fmt.Println("- " + permission)
	}
}

func describeFilePermission(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	owner, group := fileOwnerNames(info)
	return fmt.Sprintf("%s %s:%s %s", info.Mode().String(), owner, group, path), nil
}

func fileOwnerNames(info os.FileInfo) (string, string) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "unknown", "unknown"
	}
	return userNameFromID(stat.Uid), groupNameFromID(stat.Gid)
}

func userNameFromID(uid uint32) string {
	id := strconv.FormatUint(uint64(uid), 10)
	account, err := user.LookupId(id)
	if err != nil {
		return id
	}
	return account.Username
}

func groupNameFromID(gid uint32) string {
	id := strconv.FormatUint(uint64(gid), 10)
	group, err := user.LookupGroupId(id)
	if err != nil {
		return id
	}
	return group.Name
}

func lookupFileOwner(owner fileOwner) (int, int, error) {
	uid := -1
	gid := -1

	if owner.user != "" {
		value, err := lookupUserID(owner.user)
		if err != nil {
			return -1, -1, err
		}
		uid = value
	}
	if owner.group != "" {
		value, err := lookupGroupID(owner.group)
		if err != nil {
			return -1, -1, err
		}
		gid = value
	}
	return uid, gid, nil
}

func lookupUserID(name string) (int, error) {
	if name == "root" {
		return 0, nil
	}
	account, err := user.Lookup(name)
	if err != nil {
		return 0, fmt.Errorf("查找用户 %q 失败: %w", name, err)
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil {
		return 0, fmt.Errorf("解析用户 %q 的 UID 失败: %w", name, err)
	}
	return uid, nil
}

func lookupGroupID(name string) (int, error) {
	if name == "root" {
		return 0, nil
	}
	group, err := user.LookupGroup(name)
	if err != nil {
		return 0, fmt.Errorf("查找用户组 %q 失败，请确认已安装 NUT 并存在 nut 组: %w", name, err)
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil {
		return 0, fmt.Errorf("解析用户组 %q 的 GID 失败: %w", name, err)
	}
	return gid, nil
}

func backupNUTConfigFile(path string) (string, bool, error) {
	backup := originalNUTBackupPath(path)
	if system.FileExists(backup) {
		return backup, false, nil
	}

	if err := copyNUTBackupFile(path, backup); err != nil {
		return "", false, err
	}
	return backup, true, nil
}

func originalNUTBackupPath(path string) string {
	return path + ".bak"
}

func copyNUTBackupFile(source, target string) error {
	input, err := os.ReadFile(source)
	if err != nil {
		return err
	}

	info, err := os.Stat(source)
	if err != nil {
		return err
	}

	return shared.AtomicWriteFile(target, input, shared.AtomicWriteOptions{Mode: info.Mode().Perm()})
}

func restoreUPSBackup(view *ui.UI) error {
	backups := existingNUTBackups()
	if len(backups) == 0 {
		fmt.Println("未发现 UPS 配置备份文件")
		return nil
	}

	fmt.Println("将恢复以下备份文件：")
	for _, file := range backups {
		fmt.Printf("- %s -> %s\n", originalNUTBackupPath(file.path), file.path)
	}
	fmt.Println()

	confirmed, err := view.Confirm("确认恢复首次 UPS 配置备份？(y/N): ")
	if err != nil {
		return err
	}
	if !confirmed {
		fmt.Println("已取消恢复")
		return nil
	}

	for _, file := range backups {
		if err := restoreNUTBackupFile(file); err != nil {
			return err
		}
	}
	warnIfNUTConfigPermissionsMismatch()

	if err := activateNUTServices(); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("UPS 首次配置备份已恢复")
	return nil
}

func deleteUPSBackups(view *ui.UI) error {
	backups := existingNUTBackups()
	if len(backups) == 0 {
		fmt.Println("未发现 UPS 配置备份文件")
		return nil
	}

	fmt.Println("将删除以下 UPS 配置备份文件：")
	for _, file := range backups {
		fmt.Printf("- %s\n", originalNUTBackupPath(file.path))
	}
	fmt.Println()

	confirmed, err := view.Confirm("确认删除 UPS 配置备份？(y/N): ")
	if err != nil {
		return err
	}
	if !confirmed {
		fmt.Println("已取消删除")
		return nil
	}

	for _, file := range backups {
		if err := deleteNUTBackupFile(file); err != nil {
			return err
		}
	}

	fmt.Println()
	fmt.Println("UPS 配置备份已删除")
	return nil
}

func existingNUTBackups() []nutConfigFile {
	backups := make([]nutConfigFile, 0, len(nutConfigFiles))
	for _, file := range nutConfigFiles {
		if system.FileExists(originalNUTBackupPath(file.path)) {
			backups = append(backups, file)
		}
	}
	return backups
}

func deleteNUTBackupFile(file nutConfigFile) error {
	backup := originalNUTBackupPath(file.path)
	if err := os.Remove(backup); err != nil {
		return err
	}
	log.Info("已删除 NUT 配置备份：", backup)
	return nil
}

func restoreNUTBackupFile(file nutConfigFile) error {
	backup := originalNUTBackupPath(file.path)
	data, err := os.ReadFile(backup)
	if err != nil {
		return err
	}

	info, err := os.Stat(backup)
	if err != nil {
		return err
	}
	mode := info.Mode().Perm()
	if mode == 0 {
		mode = file.mode
	}

	restoredFile := file
	restoredFile.mode = mode

	options := shared.AtomicWriteOptions{Mode: restoredFile.mode}
	if shouldEnforceFileMetadata(restoredFile) {
		options.ForceMode = true
		uid, gid, ownerErr := lookupFileOwner(restoredFile.owner)
		if ownerErr != nil {
			return ownerErr
		}
		options.Owner = &shared.FileOwner{UID: uid, GID: gid}
	}
	if err := shared.AtomicWriteFile(restoredFile.path, data, options); err != nil {
		return err
	}
	if err := applyManagedFileMetadataIfNeeded(restoredFile); err != nil {
		return err
	}
	log.Info("已恢复 NUT 配置：", file.path)
	return nil
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

type nutSystemdService struct {
	unit      string
	unitFiles []string
	optional  bool
}

func activateNUTServices() error {
	for _, service := range nutSystemdServices() {
		if !nutSystemdServiceExists(service) {
			if service.optional {
				log.Warn("未检测到 ", service.unit, ".service，跳过该服务")
				continue
			}
			return fmt.Errorf("未检测到 systemd 服务: %s.service", service.unit)
		}

		log.Info("设置 ", service.unit, " 开机启动...")
		if err := system.Run("systemctl", "enable", service.unit); err != nil {
			return fmt.Errorf("设置 %s 开机启动失败: %w", service.unit, err)
		}

		log.Info("启动/重启 ", service.unit, "...")
		if err := system.Run("systemctl", "restart", service.unit); err != nil {
			return fmt.Errorf("启动/重启 %s 失败: %w", service.unit, err)
		}
	}
	return nil
}

func nutSystemdServices() []nutSystemdService {
	return []nutSystemdService{
		{
			unit:      "nut-driver@" + upsMonitorName,
			unitFiles: []string{"nut-driver@" + upsMonitorName + ".service", "nut-driver@.service"},
			optional:  true,
		},
		{unit: "nut-server"},
		{unit: "nut-monitor"},
	}
}

func nutSystemdServiceExists(service nutSystemdService) bool {
	if len(service.unitFiles) == 0 {
		return system.SystemdUnitExists(service.unit + ".service")
	}
	for _, unitFile := range service.unitFiles {
		if system.SystemdUnitExists(unitFile) {
			return true
		}
	}
	return false
}

func verifyUPSCommunication() error {
	log.Info("验证 UPS 通信：upsc ups")
	out, err := system.Output("upsc", "ups")
	if strings.TrimSpace(out) != "" {
		fmt.Println(out)
	}
	if err != nil {
		return fmt.Errorf("upsc ups 验证失败，请检查 USB 连接、vendorid/productid 和 NUT 服务日志: %w", err)
	}
	return nil
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
