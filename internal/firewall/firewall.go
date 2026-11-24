package firewall

import (
	"fmt"
	"strings"
	"time"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/firewall/forward/dockerbridge"
	"snail_tool/internal/firewall/forward/dockermark"
	"snail_tool/internal/firewall/loadedrules"
	"snail_tool/internal/firewall/portmapping"
	"snail_tool/internal/firewall/portpolicy"
	"snail_tool/internal/firewall/traffic"
	"snail_tool/internal/log"
	"snail_tool/internal/storage"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

var (
	configPath    = "/etc/nftables.conf"
	dataDir       = storage.DataDir
	commandExists = system.CommandExists
	commandRun    = system.Run
	commandOutput = system.Output
	isRoot        = system.IsRoot
)

func Run(view *ui.UI) error {
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理")
	if !isRoot() {
		return fmt.Errorf("防火墙配置需要 root 权限，请使用 sudo 运行本工具")
	}
	installed, err := ensureNftables(view)
	if err != nil {
		return err
	}
	if !installed {
		return ui.ErrReturnToMenu
	}
	for {
		ui.ClearScreen()
		ui.MenuTitle("防火墙管理")
		autostart := autostartService().Inspect()
		ui.PrintInfoCard("防火墙状态", currentConfigFields(autostart)...)
		showMenu()
		choice, err := view.Ask("请选择（回车刷新状态）：")
		if err != nil {
			return err
		}
		if ui.IsReturnChoice(choice) {
			return ui.ErrReturnToMenu
		}
		switch strings.ToLower(choice) {
		case "":
			continue
		case "1":
			ui.RunAction(view, "规则管理失败，已返回防火墙管理", func() error {
				return runAccessRuleManager(view)
			})
		case "2":
			ui.RunAction(view, "转发规则管理失败，已返回防火墙管理", func() error {
				return runForwardRuleManager(view)
			})
		case "3":
			ui.RunAction(view, "端口映射设置失败，已返回防火墙管理", func() error {
				return (portmapping.Service{
					Store: configStore(), HasManagedConfig: hasManagedConfig,
					ForwardingStatus: forwardingService().StatusField,
				}).Run(view)
			})
		case "4":
			ui.RunAction(view, "读取端口策略失败，已返回防火墙管理", (portpolicy.Service{CommandExists: commandExists, CommandOutput: commandOutput}).Show)
		case "5":
			ui.RunAction(view, "读取已加载规则失败，已返回防火墙管理", func() error { return loadedrules.Show(commandOutput) })
		case "c":
			ui.RunAction(view, "读取配置文件失败，已返回防火墙管理", configStore().Show)
		case "v":
			ui.RunAction(view, "编辑或加载配置失败，已返回防火墙管理", editConfig)
		case "r":
			ui.RunAction(view, "加载防火墙配置失败，已返回防火墙管理", func() error {
				return loadConfig(view)
			})
		case "d":
			ui.RunAction(view, "生成防火墙配置失败，已返回防火墙管理", func() error {
				return configure(view)
			})
		case "8":
			ui.RunAction(view, "常用功能设置失败，已返回防火墙管理", func() error {
				return runFeatureSettings(view)
			})
		case "9":
			ui.RunAction(view, "实时查看规则失败，已返回防火墙管理", func() error {
				return traffic.Run(view, time.Second, isRoot, commandExists)
			})
		default:
			ui.InvalidChoice()
			view.Pause()
		}
	}
}

func ensureNftables(view *ui.UI) (bool, error) {
	if !commandExists("nft") {
		confirmed, err := view.Confirm("未检测到 nft，是否安装 nftables？(y/N，0/q 返回)：")
		if err != nil {
			return false, err
		}
		if !confirmed {
			log.Info("已取消防火墙配置")
			return false, nil
		}
		if err := installNftables(); err != nil {
			return false, err
		}
		if !commandExists("nft") {
			return false, fmt.Errorf("安装后仍未检测到 nft，请检查 nftables 安装情况")
		}
	}
	return true, nil
}

func installNftables() error {
	for _, manager := range []struct {
		name string
		args []string
	}{
		{"apt-get", []string{"install", "-y", "nftables"}},
		{"apt", []string{"install", "-y", "nftables"}},
		{"dnf", []string{"install", "-y", "nftables"}},
		{"yum", []string{"install", "-y", "nftables"}},
		{"pacman", []string{"-S", "--noconfirm", "--needed", "nftables"}},
		{"zypper", []string{"--non-interactive", "install", "nftables"}},
		{"apk", []string{"add", "nftables"}},
	} {
		if !commandExists(manager.name) {
			continue
		}
		if manager.name == "apt" || manager.name == "apt-get" {
			if err := commandRun(manager.name, "update"); err != nil {
				return fmt.Errorf("更新软件包索引失败: %w", err)
			}
		}
		return commandRun(manager.name, manager.args...)
	}
	return fmt.Errorf("未识别支持的包管理器，请手动安装 nftables")
}

func configStore() configuration.Store {
	return configuration.Store{Path: configPath, DataDir: dataDir, CommandOutput: commandOutput, PreserveOriginal: preserveOriginal}
}

func editConfig() error {
	return configStore().Edit(commandExists, commandRun)
}

func loadConfig(view *ui.UI) error {
	return configStore().Reload(view, isRoot, func(content []byte) (bool, error) {
		if !commandExists("unshare") {
			return false, fmt.Errorf("缺少 unshare，无法对比配置与内核规则")
		}
		return compareConfigRules(content, inspectLiveRules())
	})
}

var dockerForwardUnitPath = dockermark.DefaultUnitPath

func dockerMarkService() dockermark.Service {
	return dockermark.Service{
		Store: configStore(), UnitPath: dockerForwardUnitPath,
		IsRoot: isRoot, CommandExists: commandExists, HasManagedConfig: hasManagedConfig,
	}
}

func dockerBridgeService() dockerbridge.Service {
	return dockerbridge.Service{
		Store: configStore(), IsRoot: isRoot, HasManagedConfig: hasManagedConfig,
		RequireDocker: dockerMarkService().RequireDocker,
	}
}

func SyncDockerForward() error { return dockerMarkService().Sync() }

func RemoveDockerForwardService() error { return dockerMarkService().RemoveService() }
