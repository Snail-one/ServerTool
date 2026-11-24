package quickconfig

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"snail_tool/internal/input"
	"snail_tool/internal/ui"
)

type Peer struct{ AllowedIPs, PresharedKey string }
type Profile struct {
	PublicKey, Addresses, ListenPort string
	Peers                            []Peer
}

func Run(view *ui.UI, name string, profile Profile, endpointValid func(string) bool, printFormat func(), submit func(string) (string, error)) error {
	endpoint, notice := "", ""
	selected := -1
	for {
		ui.ClearScreen()
		ui.MenuTitle("WireGuard", name, "管理对端", "快速配置")
		ui.PrintInfoCard("本机配置",
			ui.CardField{Label: "隧道地址", Value: profile.Addresses},
			ui.CardField{Label: "监听端口", Value: profile.ListenPort},
		)
		line := profile.PublicKey + " " + profile.Addresses
		if endpoint != "" {
			line += " " + endpoint
		}
		copyHint := "复制到另一端的「快速配置」"
		if selected >= 0 {
			if key := profile.Peers[selected].PresharedKey; key != "" {
				line += " --psk=" + key
				copyHint += fmt.Sprintf(" · 含对端 %d 的预共享密钥，请仅发给该对端", selected+1)
			} else {
				line += " --no-psk"
				copyHint += fmt.Sprintf(" · 对端 %d 未配置预共享密钥", selected+1)
			}
		} else {
			copyHint += " · 首次添加后，请复制包含对端密钥的配置回传"
		}
		fmt.Println()
		fmt.Println(copyHint)
		fmt.Println(line)
		fmt.Println()
		printFormat()
		fmt.Println()
		if notice != "" {
			ui.PrintField("提示", notice)
			notice = ""
		}
		for i, peer := range profile.Peers {
			addresses := peer.AllowedIPs
			ui.MenuOptionHint(strconv.Itoa(i+1), fmt.Sprintf("生成对端 %d 的复制行", i+1), addresses)
		}
		if len(profile.Peers) > 0 {
			fmt.Println()
		}
		ui.MenuShortcutHint("e", "填写本机端点", "可选，使用另一端能访问的主机名或 IP 和端口")
		ui.MenuExit("0/q", "返回对端列表")
		choice, err := view.Ask("粘贴对端配置，或选择上述操作：")
		if err != nil {
			return err
		}
		if ui.IsReturnChoice(choice) || choice == "" {
			return ui.ErrReturnToMenu
		}
		if strings.EqualFold(choice, "e") {
			value, err := view.Ask("请输入本机端点（主机:端口，回车清除，0/q 返回）：")
			if err != nil {
				return err
			}
			switch {
			case ui.IsReturnChoice(value):
			case value == "" || endpointValid(value):
				endpoint = value
			default:
				notice = "端点须使用主机:端口，IPv6 地址须加方括号"
			}
			continue
		}
		if number, err := input.Number(choice, 1, math.MaxInt); err == nil {
			if number <= len(profile.Peers) {
				selected = number - 1
			} else {
				notice = "请输入已显示的对端序号"
			}
			continue
		}
		message, err := submit(choice)
		if err != nil || message == "" {
			return err
		}
		notice = message
	}
}
