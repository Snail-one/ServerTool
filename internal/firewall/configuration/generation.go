package configuration

import (
	"errors"
	"fmt"
	"strings"

	"snail_tool/internal/ui"
)

type PortSelection struct {
	PublicSSH, PrivateSSH, PublicWeb, PrivateWeb bool
}

type Generation struct {
	Path, SSHPorts string
	SSHFields      []ui.CardField
	Build          func(PortSelection) (string, error)
	Review         Review
	Save           func(string) (autostart string, err error)
}

type Review struct {
	Path, Autostart, Legacy string
	Inspect                 func(string) (Preview, error)
	EditAccess, EditForward func(*ui.UI, string) (string, error)
}

type Preview struct {
	SSH, Ping, Counters string
	ExtraRules          int
	Access, Forward     []ui.CardField
}

func Generate(view *ui.UI, generation Generation) error {
	var content string
	var selection PortSelection
	for {
		write := false
		selection = PortSelection{}
		err := ui.RunPromptSteps(
			ui.PromptStep{Run: func() error {
				var err error
				selection.PublicSSH, err = AskSSH(view, "公网", generation.SSHPorts, append([]ui.CardField(nil), generation.SSHFields...)...)
				return err
			}},
			ui.PromptStep{Run: func() error {
				fields := append([]ui.CardField(nil), generation.SSHFields...)
				fields = append(fields, ui.CardField{Label: "已选公网 SSH", Value: sshChoiceLabel(selection.PublicSSH)})
				var err error
				selection.PrivateSSH, err = AskSSH(view, "局域网", generation.SSHPorts, fields...)
				return err
			}},
			ui.PromptStep{Run: func() error {
				fields := append([]ui.CardField(nil), generation.SSHFields...)
				fields = append(fields,
					ui.CardField{Label: "已选公网 SSH", Value: sshChoiceLabel(selection.PublicSSH)},
					ui.CardField{Label: "已选局域网 SSH", Value: sshChoiceLabel(selection.PrivateSSH)},
				)
				var err error
				selection.PublicWeb, err = AskWeb(view, "公网", fields...)
				return err
			}},
			ui.PromptStep{Run: func() error {
				fields := append([]ui.CardField(nil), generation.SSHFields...)
				publicState := "关闭"
				if selection.PublicWeb {
					publicState = "开放 80、443"
				}
				fields = append(fields,
					ui.CardField{Label: "已选公网 SSH", Value: sshChoiceLabel(selection.PublicSSH)},
					ui.CardField{Label: "已选局域网 SSH", Value: sshChoiceLabel(selection.PrivateSSH)},
					ui.CardField{Label: "已选公网 Web", Value: publicState},
				)
				var err error
				selection.PrivateWeb, err = AskWeb(view, "局域网", fields...)
				return err
			}},
			ui.PromptStep{Run: func() error {
				var err error
				content, err = generation.Build(selection)
				if err != nil {
					return err
				}
				write, err = generation.Review.confirm(view, &content)
				return err
			}},
		)
		if err != nil {
			return err
		}
		if write {
			break
		}
	}

	autostartAction, err := generation.Save(content)
	if err != nil {
		return err
	}
	ui.PrintSuccessCard("nftables 配置已写入并加载",
		ui.CardField{Label: "配置文件", Value: generation.Path},
		ui.CardField{Label: "SSH 端口", Value: generation.SSHPorts},
		ui.CardField{Label: "公网 SSH", Value: sshChoiceLabel(selection.PublicSSH)},
		ui.CardField{Label: "局域网 SSH", Value: sshChoiceLabel(selection.PrivateSSH)},
		ui.CardField{Label: "当前状态", Value: "配置已校验并立即生效"},
		ui.CardField{Label: "开机自启", Value: autostartAction},
		ui.CardField{Label: "查看规则", Value: "选择 4 查看端口策略，选择 5 查看全部规则"},
	)
	return nil
}

func AskSSH(view *ui.UI, scope, ports string, fields ...ui.CardField) (bool, error) {
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "生成配置", scope+" SSH 端口")
	fields = append(append([]ui.CardField(nil), fields...), ui.CardField{Label: "本步设置", Value: scope + " TCP / UDP " + ports + "，IPv4 / IPv6 同时应用"})
	ui.PrintInfoCard("生成基础配置", fields...)
	ui.MenuOptionHint("y", "开放 SSH 端口", "默认，直接回车选择")
	ui.MenuOption("n", "保持关闭")
	backLabel := "返回防火墙管理"
	if scope == "局域网" {
		backLabel = "返回公网 SSH 端口选择"
	}
	ui.MenuExit("0/q", backLabel)
	for {
		choice, err := view.Ask(fmt.Sprintf("是否开放%s SSH 端口 %s（TCP / UDP、IPv4 / IPv6 同时开放）？[Y/n，0/q 返回]：", scope, ports))
		if err != nil {
			return false, err
		}
		if ui.IsReturnChoice(choice) {
			return false, ui.ErrReturnToMenu
		}
		switch strings.ToLower(choice) {
		case "", "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Println("请输入 y 或 n，或输入 0/q 返回")
		}
	}
}

func sshChoiceLabel(open bool) string {
	if open {
		return "开放"
	}
	return "关闭"
}

func AskWeb(view *ui.UI, scope string, fields ...ui.CardField) (bool, error) {
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "生成配置", scope+" Web 端口")
	fields = append(fields, ui.CardField{Label: "本步设置", Value: scope + " TCP / UDP 80、443，IPv4 / IPv6 同时应用"})
	ui.PrintInfoCard("生成基础配置", fields...)
	ui.MenuOption("y", "开放 Web 端口 80、443")
	ui.MenuOptionHint("n", "保持关闭", "默认，直接回车选择")
	backLabel := "返回局域网 SSH 端口选择"
	if scope == "局域网" {
		backLabel = "返回公网 Web 端口选择"
	}
	ui.MenuExit("0/q", backLabel)
	for {
		choice, err := view.Ask(fmt.Sprintf("是否开放%s Web 80、443（TCP / UDP、IPv4 / IPv6 同时开放）？[y/N，0/q 返回]：", scope))
		if err != nil {
			return false, err
		}
		if ui.IsReturnChoice(choice) {
			return false, ui.ErrReturnToMenu
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

// A false result lets the caller return to the SSH and web-port choices; cancellation
// uses ErrReturnToMenu so the generation wizard returns to its preceding step.
func (review Review) confirm(view *ui.UI, draft *string) (bool, error) {
	for {
		content := *draft
		ui.ClearScreen()
		ui.MenuTitle("防火墙管理", "确认配置")
		preview, err := review.Inspect(content)
		if err != nil {
			return false, err
		}
		ui.PrintInfoCard("访问规则", preview.Access...)
		ui.PrintInfoCard("转发规则", preview.Forward...)

		writeFields := []ui.CardField{
			{Label: "SSH 端口", Value: preview.SSH},
			{Label: "附加放行规则", Value: fmt.Sprintf("保留 %d 条，可额外放行来源和端口", preview.ExtraRules)},
		}
		if text := review.Legacy; text != "" {
			writeFields = append(writeFields, ui.CardField{Label: "旧任意来源规则", Value: text})
		}
		writeFields = append(writeFields,
			ui.CardField{Label: "ICMP / ICMPv6 回显", Value: preview.Ping},
			ui.CardField{Label: "数据包统计", Value: preview.Counters},
			ui.CardField{Label: "替换范围", Value: "使用当前草稿替换原配置；原端口、内网 IP 范围及手写规则不会自动沿用"},
			ui.CardField{Label: "内核规则", Value: "重建 inet snail 表，保留其他表"},
			ui.CardField{Label: "未匹配入站流量", Value: "丢弃；保留已建立连接、回环、指定 IPv6 控制流量及 DHCPv6"},
			ui.CardField{Label: "未匹配转发流量", Value: "丢弃；已建立连接放行，新连接按转发规则名单匹配目标端口"},
			ui.CardField{Label: "配置文件", Value: review.Path, Detail: "整份替换，已有文件先备份；校验成功后写入"},
			ui.CardField{Label: "生效方式", Value: "写入后自动加载，立即生效"},
			ui.CardField{Label: "开机自启", Value: review.Autostart},
		)
		ui.PrintInfoCard("即将写入的配置", writeFields...)
		fmt.Println()
		ui.MenuOption("1", "校验、写入并立即加载")
		ui.MenuOption("2", "预览完整配置")
		ui.MenuOption("3", "重新选择 SSH / Web 端口")
		ui.MenuOptionHint("4", "自定义访问规则", "端口 / 内网 IP 范围 / 规则配置，先调整草稿")
		ui.MenuOptionHint("5", "自定义转发规则", "转发端口 / 转发内网范围 / 规则配置，先调整草稿")
		ui.MenuExit("0/q", "返回局域网 Web 端口选择")
		choice, err := view.Ask("请选择：")
		if err != nil {
			return false, err
		}
		if ui.IsReturnChoice(choice) {
			return false, ui.ErrReturnToMenu
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
		case "4":
			updated, err := review.EditAccess(view, content)
			if errors.Is(err, ui.ErrReturnToMenu) {
				continue
			}
			if err != nil {
				return false, err
			}
			*draft = updated
		case "5":
			updated, err := review.EditForward(view, content)
			if errors.Is(err, ui.ErrReturnToMenu) {
				continue
			}
			if err != nil {
				return false, err
			}
			*draft = updated
		default:
			ui.InvalidChoice()
			view.Pause()
		}
	}
}
