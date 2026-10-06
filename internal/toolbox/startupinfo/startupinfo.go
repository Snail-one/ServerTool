package startupinfo

import (
	"fmt"
	"runtime"
	"strings"

	"snail_tool/internal/startup"
	"snail_tool/internal/ui"
)

// Show displays the saved startup snapshot without running any probes again.
func Show(report *startup.Report) error {
	ui.ClearScreen()
	ui.MenuTitle("系统工具", "本次启动信息")
	if report == nil || report.FinishedAt.IsZero() {
		fmt.Println("暂无本次启动信息。")
		return nil
	}
	buildVersion := strings.TrimSpace(report.Version)
	if buildVersion == "" {
		buildVersion = "dev"
	}
	targetUser := report.TargetUser
	if targetUser == "" {
		targetUser = "未获取到"
	}
	ui.PrintInfoCard("启动概况",
		ui.CardField{Label: "启动时间", Value: report.StartedAt.Format("2006-01-02 15:04:05 MST")},
		ui.CardField{Label: "检测完成", Value: report.FinishedAt.Format("2006-01-02 15:04:05 MST")},
		ui.CardField{Label: "工具版本", Value: buildVersion},
		ui.CardField{Label: "运行平台", Value: runtime.GOOS + "/" + runtime.GOARCH},
		ui.CardField{Label: "目标用户", Value: targetUser, Detail: report.UserError},
		ui.CardField{Label: "启动总耗时", Value: fmt.Sprintf("%.3f 秒", report.FinishedAt.Sub(report.StartedAt).Seconds())},
	)
	fmt.Println()
	ui.MenuSection("启动检测耗时")
	slowest := -1
	for index, step := range report.Steps {
		if slowest < 0 || step.Elapsed > report.Steps[slowest].Elapsed {
			slowest = index
		}
	}
	for index, step := range report.Steps {
		hint := ""
		if index == slowest {
			hint = ui.PrimaryText("  ← 最耗时")
		}
		fmt.Printf("  %s %8.3f 秒%s\n", ui.TableCell(step.Label, 36), step.Elapsed.Seconds(), hint)
	}
	fmt.Println()
	ui.MenuSection("启动时检测结果")
	detected := report.Status
	ui.PrintField("SSH 公钥", userConfigurationBadge(report, detected.SSHKeys))
	ui.PrintField("SSH 安全配置", ui.ConfiguredBadge(detected.SSHSecurity))
	ui.PrintField("Vim 配置", userConfigurationBadge(report, detected.Vim))
	ui.PrintField("Bash 配置", userConfigurationBadge(report, detected.Bash))
	ui.PrintField("代理配置", userConfigurationBadge(report, detected.Proxy))
	ui.PrintField("UPS 配置", ui.ConfiguredBadge(detected.UPS))
	ui.PrintField("容器运行时", detected.Runtime)
	goVersion := detected.GoVersion
	if goVersion == "" {
		goVersion = "未检测到工具管理的当前版本"
	}
	ui.PrintField("Go 当前版本", goVersion)
	fmt.Println()
	fmt.Println(ui.MutedText("以上为本次启动的检测记录，仅在重新启动工具时更新。"))
	return nil
}

func userConfigurationBadge(report *startup.Report, configured bool) string {
	if report.TargetUser == "" {
		return ui.SoftwareBadge("未检测（用户信息不可用）", false)
	}
	return ui.ConfiguredBadge(configured)
}
