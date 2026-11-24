package wireguard

import (
	"fmt"
	"net/netip"
	"strings"

	"snail_tool/internal/ui"
	"snail_tool/internal/wireguard/interfaces/peers/quickconfig"
)

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

	profile := quickconfig.Profile{PublicKey: publicKey, Addresses: addresses, ListenPort: wireGuardValue(config.device, "listenport")}
	for _, peer := range config.peers {
		profile.Peers = append(profile.Peers, quickconfig.Peer{AllowedIPs: wireGuardValue(peer, "allowedips"), PresharedKey: wireGuardValue(peer, "presharedkey")})
	}
	return quickconfig.Run(view, name, profile, validWireGuardEndpoint, printWireGuardPeerInputFormat, func(choice string) (string, error) {
		if !isRoot() {
			return "", fmt.Errorf("写入 WireGuard 配置需要 root 权限")
		}
		if fields := strings.Fields(choice); len(fields) > 0 && fields[0] == publicKey {
			return "请输入另一端的配置", nil
		}
		peer, err := wireGuardPeerFromInput(choice)
		if err != nil {
			return err.Error(), nil
		}
		return "", saveWireGuardPeer(view, name, before, peer)
	})
}
