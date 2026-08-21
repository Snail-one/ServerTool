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
	name        string
	command     string
	packageName string
	description string
}

var commonTools = []commandLineTool{
	{name: "ripgrep", command: "rg", packageName: "ripgrep", description: "快速文本搜索"},
	{name: "jq", command: "jq", packageName: "jq", description: "JSON 处理"},
	{name: "curl", command: "curl", packageName: "curl", description: "HTTP 请求与下载"},
	{name: "wget", command: "wget", packageName: "wget", description: "文件下载"},
	{name: "tree", command: "tree", packageName: "tree", description: "目录树查看"},
	{name: "htop", command: "htop", packageName: "htop", description: "交互式进程监控"},
	{name: "tmux", command: "tmux", packageName: "tmux", description: "终端会话管理"},
	{name: "unzip", command: "unzip", packageName: "unzip", description: "ZIP 解压"},
}

var (
	commandExists = system.CommandExists
	commandRun    = system.Run
	isRoot        = system.IsRoot
)

type packageManager struct {
	name        string
	refreshArgs []string
	installArgs []string
}

// Run displays common command-line tools and installs selected missing tools.
func Run(view *ui.UI) error {
	for {
		ui.ClearScreen()
		ui.MenuTitle("系统工具", "常用命令行工具")
		for index, tool := range commonTools {
			ui.MenuOptionStatusHint(
				fmt.Sprintf("%d", index+1),
				tool.name,
				ui.InstallationBadge(commandExists(tool.command)),
				tool.command+" · "+tool.description,
			)
		}
		ui.MenuOptionHint("a", "安装全部缺失工具", "使用系统包管理器")
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
		if choice == "a" {
			shared.RunAction(view, "安装常用命令行工具失败，已返回工具菜单", func() error {
				return installMissing(view)
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
			return installSelected(view, tool)
		})
	}
}

// InstalledCount returns the number of available common tools and the total.
func InstalledCount() (int, int) {
	installed := 0
	for _, tool := range commonTools {
		if commandExists(tool.command) {
			installed++
		}
	}
	return installed, len(commonTools)
}

func installSelected(view *ui.UI, tool commandLineTool) error {
	if commandExists(tool.command) {
		ui.PrintInfoCard(tool.name+" 已安装",
			ui.CardField{Label: "命令", Value: tool.command},
			ui.CardField{Label: "用途", Value: tool.description},
		)
		return nil
	}
	return confirmAndInstall(view, []commandLineTool{tool})
}

func installMissing(view *ui.UI) error {
	missing := make([]commandLineTool, 0, len(commonTools))
	for _, tool := range commonTools {
		if !commandExists(tool.command) {
			missing = append(missing, tool)
		}
	}
	if len(missing) == 0 {
		log.Info("常用命令行工具均已安装")
		return nil
	}
	return confirmAndInstall(view, missing)
}

func confirmAndInstall(view *ui.UI, tools []commandLineTool) error {
	manager, err := detectPackageManager()
	if err != nil {
		return err
	}
	if !isRoot() {
		return fmt.Errorf("安装系统软件包需要 root 权限，请使用 sudo 运行本工具")
	}

	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, fmt.Sprintf("%s（%s）", tool.name, tool.command))
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

	if err := installPackages(manager, tools); err != nil {
		return err
	}
	missing := make([]string, 0)
	for _, tool := range tools {
		if !commandExists(tool.command) {
			missing = append(missing, tool.command)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("安装完成后仍未检测到命令：%s", strings.Join(missing, "、"))
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
		if !seen[tool.packageName] {
			packages = append(packages, tool.packageName)
			seen[tool.packageName] = true
		}
	}
	args := append(append([]string{}, manager.installArgs...), packages...)
	log.Info("安装软件包：", strings.Join(packages, "、"))
	if err := commandRun(manager.name, args...); err != nil {
		return fmt.Errorf("%s 安装软件包失败: %w", manager.name, err)
	}
	return nil
}
