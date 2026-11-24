package ups

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"snail_tool/internal/input"
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
	backupStore           = shared.BackupStore{Root: shared.DataDir, Module: "ups"}
	nutModeLine           = regexp.MustCompile(`(?m)^([ \t]*MODE[ \t]*=[ \t]*).*$`)
	nutStandaloneModeLine = regexp.MustCompile(`(?m)^[ \t]*MODE[ \t]*=[ \t]*standalone[ \t]*(?:#.*)?$`)
	usbIDLine             = regexp.MustCompile(`(?i)^[0-9a-f]{4}$`)
	rootNutOwner          = fileOwner{user: "root", group: "nut"}
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
		ui.ClearScreen()
		ui.MenuTitle("系统工具", "UPS（NUT）")
		ui.MenuOption("1", "配置或更新 UPS")
		ui.MenuOptionHint("2", "恢复首次备份", "官方默认配置")
		ui.MenuOption("3", "删除 UPS 配置备份")
		ui.MenuExit("0/q", "返回")

		choice, err := view.Ask("请选择：")
		if err != nil {
			return err
		}
		fmt.Println()

		if shared.IsReturnChoice(choice) {
			return shared.ErrReturnToMenu
		}
		switch strings.ToLower(choice) {
		case "1":
			shared.RunAction(view, "UPS 配置失败，已返回 UPS 菜单", func() error { return configureUPS(view) })
		case "2":
			shared.RunAction(view, "UPS 恢复失败，已返回 UPS 菜单", func() error { return restoreUPSBackup(view) })
		case "3":
			shared.RunAction(view, "UPS 备份删除失败，已返回 UPS 菜单", func() error { return deleteUPSBackups(view) })
		default:
			ui.InvalidChoice()
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
	ui.PrintSuccessCard("UPS 配置完成",
		ui.CardField{Label: "驱动", Value: "usbhid-ups"},
		ui.CardField{Label: "Vendor ID", Value: device.VendorID},
		ui.CardField{Label: "Product ID", Value: device.ProductID},
		ui.CardField{Label: "设备说明", Value: device.Desc},
	)
	return nil
}

func askUPSDeviceConfig(view *ui.UI) (upsDeviceConfig, error) {
	var device upsDeviceConfig
	err := ui.RunPromptSteps(
		ui.PromptStep{Run: func() error {
			value, err := askUSBID(view, "请输入 vendorid（例如 0463，0/q 返回 UPS 菜单）：")
			device.VendorID = strings.ToLower(value)
			return err
		}},
		ui.PromptStep{Run: func() error {
			value, err := askUSBID(view, "请输入 productid（例如 ffff，0/q 返回 vendorid）：")
			device.ProductID = strings.ToLower(value)
			return err
		}},
		ui.PromptStep{Run: func() error {
			value, err := view.Ask("请输入 desc（例如 SANTAK TG-BOX 850 USB UPS，0/q 返回 productid）：")
			if err != nil {
				return err
			}
			if shared.IsReturnChoice(value) {
				return shared.ErrReturnToMenu
			}
			device.Desc, err = normalizeUPSDescription(value)
			return err
		}},
	)
	return device, err
}

func askUSBID(view *ui.UI, prompt string) (string, error) {
	value, err := view.Ask(prompt)
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	if shared.IsReturnChoice(value) {
		return "", shared.ErrReturnToMenu
	}
	if !usbIDLine.MatchString(value) {
		return "", fmt.Errorf("USB ID 必须是 4 位十六进制字符")
	}
	return value, nil
}

func normalizeUPSDescription(desc string) (string, error) {
	if err := input.Text(desc); err != nil {
		return "", err
	}
	desc = strings.TrimSpace(desc)
	if len(desc) >= 2 && (desc[0] == '"' && desc[len(desc)-1] == '"' || desc[0] == '\'' && desc[len(desc)-1] == '\'') {
		desc = strings.TrimSpace(desc[1 : len(desc)-1])
	}
	if desc == "" {
		return "", fmt.Errorf("desc 不能为空")
	}
	desc = strings.ReplaceAll(desc, `\`, `\\`)
	desc = strings.ReplaceAll(desc, `"`, `\"`)
	return desc, nil
}
