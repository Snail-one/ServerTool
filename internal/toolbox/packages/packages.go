package packages

import (
	"fmt"
	"sort"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

type commandLineTool struct {
	name              string
	command           string
	packageName       string
	description       string
	extraCommands     []string
	packagesByManager map[string][]string
}

var commonTools = []commandLineTool{
	{name: "curl", command: "curl", packageName: "curl", description: "HTTP 请求与下载"},
	{name: "wget", command: "wget", packageName: "wget", description: "文件下载"},
	{name: "bash-completion", packageName: "bash-completion", description: "Bash 命令自动补全"},
	{name: "tmux", command: "tmux", packageName: "tmux", description: "终端会话管理"},
	{name: "btop", command: "btop", packageName: "btop", description: "交互式资源监控"},
	{name: "unzip", command: "unzip", packageName: "unzip", description: "ZIP 解压"},
	{name: "jq", command: "jq", packageName: "jq", description: "JSON 处理"},
	{name: "ripgrep", command: "rg", packageName: "ripgrep", description: "快速文本搜索"},
	{name: "tree", command: "tree", packageName: "tree", description: "目录树查看"},
	{
		name: "build-essential", command: "gcc", packageName: "build-essential",
		description: "C/C++ 编译工具", extraCommands: []string{"g++", "make"},
		packagesByManager: map[string][]string{
			"dnf":    {"gcc", "gcc-c++", "make"},
			"yum":    {"gcc", "gcc-c++", "make"},
			"pacman": {"base-devel"},
			"zypper": {"gcc", "gcc-c++", "make"},
			"apk":    {"build-base"},
		},
	},
}

var (
	commandExists = system.CommandExists
	commandRun    = system.Run
	commandOutput = packageQueryOutput
	isRoot        = system.IsRoot
)

type packageManager struct {
	name        string
	refreshArgs []string
	installArgs []string
}

// Run displays common command-line tools and installs selected missing tools.
func Run(view *ui.UI) error {
	return NewInventory().Run(view)
}

func (inventory *Inventory) Run(view *ui.UI) error {
	for {
		ui.ClearScreen()
		ui.MenuTitle("系统工具", "常用命令行工具")
		if inventory.err != nil {
			log.Warn("检测失败：", inventory.err)
		}
		for index, tool := range commonTools {
			ui.MenuOptionStatusHint(
				fmt.Sprintf("%d", index+1),
				tool.name,
				inventory.badge(tool),
				tool.commandLabel()+" · "+tool.description,
			)
		}
		ui.MenuOptionHint("a", "安装全部缺失工具", "使用系统包管理器")
		ui.MenuOption("r", "刷新安装状态")
		ui.MenuExit("0/q", "返回")
		fmt.Println()

		choice, err := view.Ask("请选择：")
		if err != nil {
			return err
		}
		fmt.Println()

		choice = strings.ToLower(strings.TrimSpace(choice))
		if shared.IsReturnChoice(choice) {
			return shared.ErrReturnToMenu
		}
		if choice == "r" {
			_ = inventory.Refresh()
			continue
		}
		if choice == "a" {
			shared.RunAction(view, "安装常用命令行工具失败，已返回工具菜单", func() error {
				return inventory.installMissing(view)
			})
			continue
		}

		selected := -1
		for index := range commonTools {
			if choice == fmt.Sprintf("%d", index+1) {
				selected = index
				break
			}
		}
		if selected < 0 {
			ui.InvalidChoice()
			view.Pause()
			continue
		}
		tool := commonTools[selected]
		shared.RunAction(view, "安装 "+tool.name+" 失败，已返回工具菜单", func() error {
			return inventory.installSelected(view, tool)
		})
	}
}

func (inventory *Inventory) badge(tool commandLineTool) string {
	if inventory.err != nil {
		return ui.SoftwareBadge("检测失败", false)
	}
	return ui.InstallationBadge(inventory.toolInstalled(tool))
}

func (inventory *Inventory) installSelected(view *ui.UI, tool commandLineTool) error {
	if inventory.err != nil {
		return fmt.Errorf("检测失败，请刷新安装状态后重试: %w", inventory.err)
	}
	if inventory.toolInstalled(tool) {
		label := "命令"
		if tool.command == "" {
			label = "软件包"
		}
		ui.PrintInfoCard(tool.name+" 已安装",
			ui.CardField{Label: label, Value: tool.commandLabel()},
			ui.CardField{Label: "用途", Value: tool.description},
		)
		return nil
	}
	return inventory.confirmAndInstall(view, []commandLineTool{tool})
}

func (inventory *Inventory) installMissing(view *ui.UI) error {
	if inventory.err != nil {
		return fmt.Errorf("检测失败，请刷新安装状态后重试: %w", inventory.err)
	}
	missing := make([]commandLineTool, 0, len(commonTools))
	for _, tool := range commonTools {
		if !inventory.toolInstalled(tool) {
			missing = append(missing, tool)
		}
	}
	if len(missing) == 0 {
		log.Info("常用命令行工具均已安装")
		return nil
	}
	return inventory.confirmAndInstall(view, missing)
}

func (inventory *Inventory) confirmAndInstall(view *ui.UI, tools []commandLineTool) error {
	manager := inventory.manager
	if !isRoot() {
		return fmt.Errorf("安装系统软件包需要 root 权限，请使用 sudo 运行本工具")
	}

	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, fmt.Sprintf("%s（%s）", tool.name, tool.commandLabel()))
	}
	sort.Strings(names)
	confirmed, err := view.Confirm(fmt.Sprintf(
		"将使用 %s 安装 %s，是否继续？(y/N)：",
		manager.name,
		strings.Join(names, "、"),
	))
	if err != nil {
		return err
	}
	if !confirmed {
		log.Info("已取消安装")
		return nil
	}

	installErr := installPackages(manager, tools)
	queryErr := inventory.Refresh()
	if installErr != nil {
		return installErr
	}
	if queryErr != nil {
		return fmt.Errorf("安装完成，但检测失败: %w", queryErr)
	}
	missing := make([]string, 0)
	for _, tool := range tools {
		if !inventory.toolInstalled(tool) {
			missing = append(missing, tool.name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("安装完成后仍未检测到工具：%s", strings.Join(missing, "、"))
	}

	ui.PrintSuccessCard("常用命令行工具安装完成",
		ui.CardField{Label: "包管理器", Value: manager.name},
		ui.CardField{Label: "已安装", Value: strings.Join(names, "、")},
	)
	return nil
}

func detectPackageManager() (packageManager, error) {
	for _, candidate := range []packageManager{
		{name: "apt-get", refreshArgs: []string{"update"}, installArgs: []string{"install", "-y"}},
		{name: "apt", refreshArgs: []string{"update"}, installArgs: []string{"install", "-y"}},
		{name: "dnf", installArgs: []string{"install", "-y"}},
		{name: "yum", installArgs: []string{"install", "-y"}},
		{name: "pacman", installArgs: []string{"-Sy", "--noconfirm", "--needed"}},
		{name: "zypper", installArgs: []string{"--non-interactive", "install"}},
		{name: "apk", installArgs: []string{"add"}},
	} {
		if commandExists(candidate.name) {
			return candidate, nil
		}
	}
	return packageManager{}, fmt.Errorf("未识别支持的包管理器，请手动安装所需工具")
}

func installPackages(manager packageManager, tools []commandLineTool) error {
	if len(manager.refreshArgs) > 0 {
		log.Info("更新软件包索引...")
		if err := commandRun(manager.name, manager.refreshArgs...); err != nil {
			return fmt.Errorf("%s 更新软件包索引失败: %w", manager.name, err)
		}
	}

	packages := make([]string, 0, len(tools))
	seen := make(map[string]bool)
	for _, tool := range tools {
		for _, name := range tool.packageNames(manager) {
			if !seen[name] {
				packages = append(packages, name)
				seen[name] = true
			}
		}
	}
	args := append(append([]string{}, manager.installArgs...), packages...)
	log.Info("安装软件包：", strings.Join(packages, "、"))
	if err := commandRun(manager.name, args...); err != nil {
		return fmt.Errorf("%s 安装软件包失败: %w", manager.name, err)
	}
	return nil
}

func (tool commandLineTool) commandLabel() string {
	if tool.command == "" {
		return tool.packageName
	}
	return strings.Join(append([]string{tool.command}, tool.extraCommands...), " / ")
}

func (tool commandLineTool) packageNames(manager packageManager) []string {
	if names := tool.packagesByManager[manager.name]; len(names) > 0 {
		return names
	}
	return []string{tool.packageName}
}
