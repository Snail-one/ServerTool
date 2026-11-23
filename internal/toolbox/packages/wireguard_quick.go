package packages

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

func quickWireGuardConfig(view *ui.UI, name string) error {
	before, err := readWireGuardProfile(name)
	if err != nil {
		return err
	}
	config, err := parseWireGuardConfig(before)
	if err != nil {
		return err
	}
	publicKey := wireGuardPublicKey(config.device)
	if publicKey == "" {
		return fmt.Errorf("配置缺少接口私钥，无法生成本机公钥")
	}
	addresses, err := wireGuardInterfacePrefixes(config.device)
	if err != nil {
		return err
	}

	endpoint, notice := "", ""
	selected := -1
	for {
		ui.ClearScreen()
		ui.MenuTitle("WireGuard", name, "快速配置")
		ui.PrintInfoCard("本机配置",
			ui.CardField{Label: "隧道地址", Value: addresses},
			ui.CardField{Label: "监听端口", Value: wireGuardValue(config.device, "listenport")},
		)
		line := publicKey + " " + addresses
		if endpoint != "" {
			line += " " + endpoint
		}
		copyHint := "复制到另一端的「快速配置」或「3 添加对端」"
		if selected >= 0 {
			if key := wireGuardValue(config.peers[selected], "presharedkey"); key != "" {
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
		if notice != "" {
			ui.PrintField("提示", notice)
			notice = ""
		}
		for i, peer := range config.peers {
			addresses := wireGuardValue(peer, "allowedips")
			ui.MenuOptionHint(strconv.Itoa(i+1), fmt.Sprintf("生成对端 %d 的复制行", i+1), addresses)
		}
		ui.MenuShortcutHint("e", "填写本机端点", "可选，使用另一端能访问的主机名或 IP 和端口")
		ui.MenuExit("0/q", "返回接口菜单")
		choice, err := view.Ask("粘贴对端配置，或选择上述操作：")
		if err != nil {
			return err
		}
		if shared.IsReturnChoice(choice) || choice == "" {
			return shared.ErrReturnToMenu
		}
		if strings.EqualFold(choice, "e") {
			value, err := view.Ask("请输入本机端点（主机:端口，回车清除，0/q 返回）：")
			if err != nil {
				return err
			}
			switch {
			case shared.IsReturnChoice(value):
			case value == "" || validWireGuardEndpoint(value):
				endpoint = value
			default:
				notice = "端点须使用主机:端口，IPv6 地址须加方括号"
			}
			continue
		}
		if number, err := strconv.Atoi(choice); err == nil {
			if number >= 1 && number <= len(config.peers) {
				selected = number - 1
			} else {
				notice = "请输入已显示的对端序号"
			}
			continue
		}
		if !isRoot() {
			return fmt.Errorf("写入 WireGuard 配置需要 root 权限")
		}
		if fields := strings.Fields(choice); len(fields) > 0 && fields[0] == publicKey {
			notice = "请输入另一端的配置"
			continue
		}
		peer, err := wireGuardPeerFromInput(choice)
		if err != nil {
			notice = err.Error()
			continue
		}
		return saveWireGuardPeer(view, name, before, peer)
	}
}

func wireGuardInterfacePrefixes(settings []wireGuardSetting) (string, error) {
	var prefixes []string
	seen := make(map[netip.Addr]bool)
	for _, setting := range settings {
		if setting.key != "address" {
			continue
		}
		for _, value := range strings.Split(setting.value, ",") {
			prefix, err := parseWireGuardAddress(value)
			if err != nil {
				return "", fmt.Errorf("接口的 Address 格式无效")
			}
			ip := prefix.Addr()
			if !seen[ip] {
				prefixes = append(prefixes, netip.PrefixFrom(ip, ip.BitLen()).String())
				seen[ip] = true
			}
		}
	}
	if len(prefixes) == 0 {
		return "", fmt.Errorf("请先在接口配置中填写隧道地址 Address")
	}
	return strings.Join(prefixes, ","), nil
}
