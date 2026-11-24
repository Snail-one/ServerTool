package firewall

import (
	"fmt"
	"os"
	"strings"
	"time"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

var (
	configPath    = "/etc/nftables.conf"
	dataDir       = shared.DataDir
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
		return shared.ErrReturnToMenu
	}
	for {
		ui.ClearScreen()
		ui.MenuTitle("防火墙管理")
		autostart := inspectAutostart()
		ui.PrintInfoCard("防火墙状态", currentConfigFields(autostart)...)
		showMenu()
		choice, err := view.Ask("请选择（回车刷新状态）：")
		if err != nil {
			return err
		}
		if shared.IsReturnChoice(choice) {
			return shared.ErrReturnToMenu
		}
		switch strings.ToLower(choice) {
		case "":
			continue
		case "1":
			shared.RunAction(view, "规则管理失败，已返回防火墙管理", func() error {
				return runAccessRuleManager(view)
			})
		case "2":
			shared.RunAction(view, "转发规则管理失败，已返回防火墙管理", func() error {
				return runForwardRuleManager(view)
			})
		case "3":
			shared.RunAction(view, "端口映射设置失败，已返回防火墙管理", func() error {
				return runPortMappingManager(view)
			})
		case "4":
			shared.RunAction(view, "读取端口策略失败，已返回防火墙管理", showLoadedPorts)
		case "5":
			shared.RunAction(view, "读取已加载规则失败，已返回防火墙管理", showLoadedRules)
		case "c":
			shared.RunAction(view, "读取配置文件失败，已返回防火墙管理", showConfigFile)
		case "v":
			shared.RunAction(view, "编辑或加载配置失败，已返回防火墙管理", editConfig)
		case "r":
			shared.RunAction(view, "加载防火墙配置失败，已返回防火墙管理", func() error {
				return loadConfig(view)
			})
		case "d":
			shared.RunAction(view, "生成防火墙配置失败，已返回防火墙管理", func() error {
				return configure(view)
			})
		case "8":
			shared.RunAction(view, "常用功能设置失败，已返回防火墙管理", func() error {
				return runFeatureSettings(view)
			})
		case "9":
			shared.RunAction(view, "实时查看规则失败，已返回防火墙管理", func() error {
				return monitorTraffic(view, time.Second)
			})
		default:
			ui.InvalidChoice()
			view.Pause()
		}
	}
}

func showConfigFile() error {
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "配置文件")
	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		ui.PrintInfoCard("尚未生成防火墙配置", ui.CardField{Label: "下一步", Value: "选择 d 生成默认配置"})
		return nil
	}
	if err != nil {
		return err
	}
	ui.PrintField("配置文件", configPath)
	fmt.Println(string(data))
	return nil
}

func showLoadedRules() error {
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "全部已加载规则")
	out, err := commandOutput("nft", "list", "ruleset")
	if err != nil {
		return fmt.Errorf("读取完整 nftables 规则集失败: %w: %s", err, strings.TrimSpace(out))
	}
	if strings.TrimSpace(out) == "" {
		ui.PrintInfoCard("当前没有已加载的 nftables 规则")
	} else {
		fmt.Println(out)
	}
	return nil
}

func loadConfig(view *ui.UI) error {
	if !isRoot() {
		return fmt.Errorf("加载防火墙配置需要 root 权限，请使用 sudo 运行本工具")
	}
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "校验加载配置")
	info, err := os.Lstat(configPath)
	if os.IsNotExist(err) {
		return fmt.Errorf("未找到 %s，请先生成或编辑配置", configPath)
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("拒绝加载非普通文件或软链接: %s", configPath)
	}
	content, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	version := configVersion{content: content, exists: true}
	if err := validateSavedConfig(version); err != nil {
		return err
	}
	equal := false
	var compareErr error
	if commandExists("unshare") {
		equal, compareErr = compareConfigRules(content, inspectLiveRules())
	} else {
		compareErr = fmt.Errorf("缺少 unshare，无法对比配置与内核规则")
	}
	if err := checkConfigVersion(version); err != nil {
		return err
	}
	if compareErr == nil && equal {
		ui.PrintSuccessCard("配置已同步，无需加载",
			ui.CardField{Label: "校验结果", Value: "通过"},
			ui.CardField{Label: "配置对比", Value: "当前文件与内核规则一致"},
		)
		return nil
	}
	fields := []ui.CardField{
		{Label: "配置文件", Value: configPath},
		{Label: "校验结果", Value: "通过"},
	}
	if compareErr != nil {
		fields = append(fields, ui.CardField{Label: "配置对比", Value: "无法比较", Detail: compareErr.Error()})
	} else {
		fields = append(fields, ui.CardField{Label: "配置对比", Value: "存在差异，需要加载才能同步"})
	}
	fields = append(fields,
		ui.CardField{Label: "生效范围", Value: "立即执行文件中的规则，影响新建网络连接"},
	)
	ui.PrintInfoCard("准备加载防火墙配置", fields...)
	confirmed, err := view.Confirm("是否立即加载此防火墙配置？(y/N，0/q 返回)：")
	if err != nil {
		return err
	}
	if !confirmed {
		log.Info("配置文件已保留，未加载；可稍后输入 r 校验加载配置")
		return shared.ErrReturnToMenu
	}
	return loadValidatedConfig(content)
}

func loadValidatedConfig(content []byte) error {
	if err := applyConfig(content); err != nil {
		return err
	}
	ui.PrintSuccessCard("防火墙配置已加载",
		ui.CardField{Label: "配置文件", Value: configPath},
		ui.CardField{Label: "当前状态", Value: "已立即生效"},
		ui.CardField{Label: "查看规则", Value: "选择 4 查看端口策略，选择 5 查看全部规则"},
	)
	return nil
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
