package firewall

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/tempguard"
	"snail_tool/internal/ui"
)

//go:embed nftables.conf
var configTemplate string

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
			shared.RunAction(view, "读取端口策略失败，已返回防火墙管理", showLoadedPorts)
		case "2":
			shared.RunAction(view, "读取已加载规则失败，已返回防火墙管理", showLoadedRules)
		case "3":
			shared.RunAction(view, "规则管理失败，已返回防火墙管理", func() error {
				return runRuleManager(view)
			})
		case "4":
			shared.RunAction(view, "读取配置文件失败，已返回防火墙管理", showConfigFile)
		case "5":
			shared.RunAction(view, "编辑或加载配置失败，已返回防火墙管理", func() error {
				return editConfig(view)
			})
		case "r":
			shared.RunAction(view, "加载防火墙配置失败，已返回防火墙管理", func() error {
				return loadConfig(view)
			})
		case "6":
			shared.RunAction(view, "生成防火墙配置失败，已返回防火墙管理", func() error {
				return configure(view)
			})
		case "7":
			shared.RunAction(view, "常用功能设置失败，已返回防火墙管理", func() error {
				return runFeatureSettings(view)
			})
		case "8":
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
		ui.PrintInfoCard("尚未生成防火墙配置", ui.CardField{Label: "下一步", Value: "选择 6 生成默认配置"})
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
	if len(bytes.TrimSpace(content)) == 0 {
		return fmt.Errorf("防火墙配置文件为空，请先生成或编辑配置")
	}
	log.Info("正在校验最新防火墙配置：", configPath)
	if out, err := commandOutput("nft", "-c", "-f", configPath); err != nil {
		return fmt.Errorf("nftables 配置校验失败，未加载规则: %w: %s", err, strings.TrimSpace(out))
	}
	equal := false
	var compareErr error
	if commandExists("unshare") {
		equal, compareErr = compareConfigRules(content, inspectLiveRules())
	} else {
		compareErr = fmt.Errorf("缺少 unshare，无法对比配置与内核规则")
	}
	if err := checkConfigVersion(configVersion{content: content, exists: true}); err != nil {
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
	confirmed, err := view.Confirm("是否立即加载此防火墙配置？(y/N)：")
	if err != nil {
		return err
	}
	if !confirmed {
		log.Info("配置文件已保留，未加载；可稍后输入 r 校验加载配置")
		return shared.ErrReturnToMenu
	}
	if err := applyConfig(content); err != nil {
		return err
	}
	ui.PrintSuccessCard("防火墙配置已加载",
		ui.CardField{Label: "配置文件", Value: configPath},
		ui.CardField{Label: "当前状态", Value: "已立即生效"},
		ui.CardField{Label: "查看规则", Value: "选择 1 查看端口策略，选择 2 查看全部规则"},
	)
	return nil
}

func applyConfig(expected []byte) error {
	latest, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	if !bytes.Equal(expected, latest) {
		return fmt.Errorf("配置文件已变化，未加载规则，请重新选择校验加载配置")
	}
	if err := preserveOriginal(); err != nil {
		return err
	}
	if out, err := commandOutput("nft", "-f", configPath); err != nil {
		return fmt.Errorf("nftables 配置加载失败: %w: %s", err, strings.TrimSpace(out))
	}
	return nil
}

func editConfig(view *ui.UI) error {
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "编辑配置文件")
	var editor string
	for _, name := range []string{"vim", "vi", "nano"} {
		if commandExists(name) {
			editor = name
			break
		}
	}
	if editor == "" {
		return fmt.Errorf("未找到 vim、vi、nano 中的任意工具，请先安装其中一个编辑器")
	}
	before, err := readConfigVersion()
	if err != nil {
		return err
	}
	if err := preserveOriginal(); err != nil {
		return err
	}
	if before.exists {
		backup, err := backupFirewallConfig()
		if err != nil {
			return err
		}
		log.Info("已备份原防火墙配置：", backup)
	}
	if err := commandRun(editor, configPath); err != nil {
		return fmt.Errorf("%s 编辑失败: %w", editor, err)
	}
	after, err := readConfigVersion()
	if err != nil {
		return err
	}
	if before.exists == after.exists && bytes.Equal(before.content, after.content) {
		log.Info("配置内容未修改，无需加载")
		return nil
	}
	if err := loadConfig(view); err != nil {
		return fmt.Errorf("编辑内容已保留，请根据错误修正或重新加载: %w", err)
	}
	return nil
}

func ensureNftables(view *ui.UI) (bool, error) {
	if !commandExists("nft") {
		confirmed, err := view.Confirm("未检测到 nft，是否安装 nftables？(y/N)：")
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

func configure(view *ui.UI) error {
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
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "生成配置")
	version, err := readConfigVersion()
	if err != nil {
		return err
	}
	var preserved []allowRule
	countersEnabled, err := packetCountersEnabled(string(version.content))
	if err != nil {
		return err
	}
	pingEnabled, pingFound, err := readPingSetting(string(version.content))
	if err != nil {
		return err
	}
	if strings.Contains(string(version.content), rulesBegin) || strings.Contains(string(version.content), rulesEnd) {
		preserved, err = managedRules(string(version.content))
		if err != nil {
			return err
		}
	}
	ports, err := detectSSHPorts()
	if err != nil {
		return err
	}
	if len(ports) > 1 {
		log.Warn("SSH 生效配置包含多个端口：", portList(ports), "。通常建议只保留一个 SSH 端口；本次会全部放行，请在 SSH 安全配置中检查并关闭不再使用的旧端口。")
	}
	ui.PrintInfoCard("检测到 SSH 配置端口",
		ui.CardField{Label: "端口", Value: portList(ports), Detail: "/usr/sbin/sshd -T"},
		ui.CardField{Label: "开放范围", Value: "公网、内网及 IPv4 / IPv6"},
		ui.CardField{Label: "放行协议", Value: "相同端口号同时放行 TCP / UDP，列表独立维护"},
		ui.CardField{Label: "附加放行规则", Value: fmt.Sprintf("保留已有 %d 条规则", len(preserved))},
	)
	var content string
	for {
		publicWeb, err := askWeb(view, "公网")
		if err != nil {
			return err
		}
		privateWeb, err := askWeb(view, "局域网")
		if err != nil {
			return err
		}
		content, err = buildConfig(ports, publicWeb, privateWeb)
		if err != nil {
			return err
		}
		content, err = replaceManagedRules(content, preserved)
		if err != nil {
			return err
		}
		if version.exists && !pingFound {
			content, err = withoutPingSetting(content)
		} else {
			content, err = withPingSetting(content, pingEnabled)
		}
		if err != nil {
			return err
		}
		content, err = withPacketCounters(content, countersEnabled)
		if err != nil {
			return err
		}
		write, err := confirmConfig(view, content, ports, publicWeb, privateWeb)
		if err != nil {
			return err
		}
		if write {
			break
		}
	}
	// Detect again after the interaction so a changed SSH listener cannot be
	// silently omitted from the generated firewall.
	latest, err := detectSSHPorts()
	if err != nil {
		return err
	}
	if portList(latest) != portList(ports) {
		return fmt.Errorf("SSH 配置端口已变化，请重新进入防火墙管理生成配置")
	}
	if err := writeConfig(content, version); err != nil {
		return err
	}
	if err := applyConfig([]byte(content)); err != nil {
		return fmt.Errorf("配置已写入 %s，但未成功加载: %w", configPath, err)
	}
	ui.PrintSuccessCard("nftables 配置已写入并加载",
		ui.CardField{Label: "配置文件", Value: configPath},
		ui.CardField{Label: "SSH 端口", Value: portList(ports)},
		ui.CardField{Label: "当前状态", Value: "配置已校验并立即生效"},
		ui.CardField{Label: "查看规则", Value: "选择 1 查看端口策略，选择 2 查看全部规则"},
	)
	return nil
}

func askWeb(view *ui.UI, scope string) (bool, error) {
	for {
		choice, err := view.Ask(fmt.Sprintf("是否开放%s Web 80、443（TCP / UDP、IPv4 / IPv6 同时开放）？[y/N，0/q 返回]：", scope))
		if err != nil {
			return false, err
		}
		if shared.IsReturnChoice(choice) {
			return false, shared.ErrReturnToMenu
		}
		switch strings.ToLower(choice) {
		case "y", "yes":
			return true, nil
		case "", "n", "no":
			return false, nil
		default:
			fmt.Println("请输入 y 或 n，或输入 0/q 返回")
		}
	}
}

// A false result lets the caller return to the web-port choices; cancellation
// uses ErrReturnToMenu so RunAction returns directly to the firewall menu.
func confirmConfig(view *ui.UI, content string, ports []int, publicWeb, privateWeb bool) (bool, error) {
	for {
		ui.ClearScreen()
		ui.MenuTitle("防火墙管理", "确认配置")
		publicPorts := portList(ports)
		privatePorts := portList(ports)
		if publicWeb {
			publicPorts += ", 80, 443"
		}
		if privateWeb {
			privatePorts += ", 80, 443"
		}
		extraRules, err := managedRules(content)
		if err != nil {
			return false, err
		}
		pingEnabled, pingFound, err := readPingSetting(content)
		if err != nil {
			return false, err
		}
		pingState := "沿用原有规则"
		if pingFound {
			pingState = pingLabel(pingEnabled)
		}
		counters, err := packetCountersEnabled(content)
		if err != nil {
			return false, err
		}
		counterState := "已关闭"
		if counters {
			counterState = ui.PrimaryText("已开启")
		}
		ui.PrintInfoCard("即将写入的配置",
			ui.CardField{Label: "公网 TCP", Value: publicPorts, Detail: "IPv4 / IPv6 同时应用"},
			ui.CardField{Label: "公网 UDP", Value: publicPorts, Detail: "与 TCP 同时选择，列表独立保留"},
			ui.CardField{Label: "局域网 TCP", Value: privatePorts, Detail: "IPv4 私有地址；IPv6 ULA 和链路本地地址"},
			ui.CardField{Label: "局域网 UDP", Value: privatePorts, Detail: "与 TCP 同时选择，列表独立保留"},
			ui.CardField{Label: "附加放行规则", Value: fmt.Sprintf("保留 %d 条，可额外放行来源和端口", len(extraRules))},
			ui.CardField{Label: "ICMP / ICMPv6 回显", Value: pingState},
			ui.CardField{Label: "数据包统计", Value: counterState},
			ui.CardField{Label: "手动修改", Value: "端口列表、来源例外等将由新模板覆盖"},
			ui.CardField{Label: "内核规则", Value: "重建 inet snail 表，保留其他表"},
			ui.CardField{Label: "未匹配入站流量", Value: "丢弃；保留已建立连接、回环、指定 IPv6 控制流量及 DHCPv6"},
			ui.CardField{Label: "配置文件", Value: configPath, Detail: "整份替换，已有文件先备份；校验成功后写入"},
			ui.CardField{Label: "生效方式", Value: "写入后自动加载，立即生效"},
		)
		fmt.Println()
		ui.MenuOption("1", "校验、写入并立即加载")
		ui.MenuOption("2", "预览完整配置")
		ui.MenuOption("3", "重新选择公网 / 局域网 Web 端口")
		ui.MenuExit("0/q", "取消并返回")
		fmt.Println()
		choice, err := view.Ask("请选择：")
		if err != nil {
			return false, err
		}
		if shared.IsReturnChoice(choice) {
			return false, shared.ErrReturnToMenu
		}
		switch choice {
		case "1":
			return true, nil
		case "2":
			ui.ClearScreen()
			ui.MenuTitle("防火墙管理", "配置预览")
			fmt.Println(content)
			view.PauseWithPrompt("按回车返回配置确认…")
		case "3":
			return false, nil
		default:
			ui.InvalidChoice()
			view.Pause()
		}
	}
}

func buildConfig(ports []int, publicWeb, privateWeb bool) (string, error) {
	ports, err := mergeSSHPorts(ports)
	if err != nil {
		return "", err
	}
	publicPorts, privatePorts := "$snail_ssh_port", "$snail_ssh_port"
	publicDescription, privateDescription := "当前仅开放 SSH 端口", "当前仅开放 SSH 端口"
	if publicWeb {
		publicPorts += ", 80, 443"
		publicDescription = "当前开放 SSH 端口及 80、443"
	}
	if privateWeb {
		privatePorts += ", 80, 443"
		privateDescription = "当前开放 SSH 端口及 80、443"
	}
	content := strings.NewReplacer(
		"__SSH_PORTS__", portList(ports),
		"__PUBLIC_PORTS__", publicPorts,
		"__PRIVATE_PORTS__", privatePorts,
		"__PUBLIC_PORT_DESCRIPTION__", publicDescription,
		"__PRIVATE_PORT_DESCRIPTION__", privateDescription,
	).Replace(configTemplate)
	return withPingSetting(content, false)
}

func portList(ports []int) string {
	values := make([]string, len(ports))
	for i, port := range ports {
		values[i] = strconv.Itoa(port)
	}
	return strings.Join(values, ", ")
}

func writeConfig(content string, versions ...configVersion) error {
	release, err := tempguard.Use()
	if err != nil {
		return err
	}
	defer release()

	if info, err := os.Lstat(configPath); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("拒绝覆盖非普通文件或软链接: %s", configPath)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(configPath), ".snail-nftables-*.conf")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if out, err := commandOutput("nft", "-c", "-f", tmp.Name()); err != nil {
		return fmt.Errorf("nftables 配置校验失败，未写入正式配置: %w: %s", err, strings.TrimSpace(out))
	}
	if len(versions) > 0 {
		if err := checkConfigVersion(versions[0]); err != nil {
			return err
		}
	}
	if err := preserveOriginal(); err != nil {
		return err
	}
	if len(versions) > 0 {
		if err := checkConfigVersion(versions[0]); err != nil {
			return err
		}
	}
	if _, err := os.Lstat(configPath); err == nil {
		backup, err := backupFirewallConfig()
		if err != nil {
			return err
		}
		log.Info("已备份原防火墙配置：", backup)
	} else if !os.IsNotExist(err) {
		return err
	}
	return shared.AtomicWriteFile(configPath, []byte(content), shared.AtomicWriteOptions{Mode: 0644})
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
