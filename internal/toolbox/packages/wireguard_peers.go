package packages

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

const defaultWireGuardKeepalive = "0"

var wireGuardPeerOptions = [][2]string{
	{"PublicKey", "公钥"},
	{"AllowedIPs", "允许地址"},
	{"Endpoint", "连接端点"},
	{"PersistentKeepalive", "保活间隔（秒）"},
	{"PresharedKey", "预共享密钥"},
}

func wireGuardPeerFields(peer []wireGuardSetting) []ui.CardField {
	fields := wireGuardFields(peer, [][2]string{
		{"publickey", "公钥"}, {"endpoint", "端点"}, {"allowedips", "允许地址"},
		{"persistentkeepalive", "保活间隔"},
	})
	if wireGuardValue(peer, "presharedkey") != "" {
		fields = append(fields, ui.CardField{Label: "预共享密钥", Value: "已配置"})
	}
	return fields
}

func manageWireGuardPeers(view *ui.UI, name string) error {
	notice := ""
	for {
		before, err := readWireGuardProfile(name)
		if err != nil {
			return err
		}
		config, err := parseWireGuardConfig(before)
		if err != nil {
			return err
		}
		ui.ClearScreen()
		ui.MenuTitle("WireGuard", name, "管理对端")
		withPSK := 0
		for _, peer := range config.peers {
			if wireGuardValue(peer, "presharedkey") != "" {
				withPSK++
			}
		}
		ui.PrintInfoCard("对端概况",
			ui.CardField{Label: "已保存对端", Value: fmt.Sprintf("%d 个", len(config.peers))},
			ui.CardField{Label: "已配置预共享密钥", Value: fmt.Sprintf("%d 个", withPSK)},
		)
		fmt.Println()
		if notice != "" {
			ui.PrintField("提示", notice)
			notice = ""
		}
		for i, peer := range config.peers {
			ui.MenuOptionHint(strconv.Itoa(i+1), fmt.Sprintf("对端 %d", i+1), wireGuardPeerValue(peer, "AllowedIPs"))
		}
		if len(config.peers) > 0 {
			fmt.Println()
		}
		ui.MenuShortcutHint("g", "快速配置", "复制本机配置或粘贴对端配置")
		ui.MenuExit("0/q", "返回接口菜单")
		choice, err := view.Ask("请选择对端或操作：")
		if err != nil {
			return err
		}
		if shared.IsReturnChoice(choice) || choice == "" && len(config.peers) == 0 {
			return shared.ErrReturnToMenu
		}
		if strings.EqualFold(choice, "g") {
			shared.RunAction(view, "快速配置失败，已返回对端列表", func() error {
				return quickWireGuardConfig(view, name)
			})
			continue
		}
		number, err := strconv.Atoi(choice)
		if err != nil || number < 1 || number > len(config.peers) {
			notice = "请输入对端序号或快捷键"
			continue
		}
		shared.RunAction(view, "对端操作失败，已返回对端列表", func() error {
			return manageWireGuardPeer(view, name, before, config, number-1)
		})
	}
}

func manageWireGuardPeer(view *ui.UI, name string, before []byte, config wireGuardConfig, index int) error {
	peer := config.peers[index]
	for {
		ui.ClearScreen()
		ui.MenuTitle("WireGuard", name, "管理对端", fmt.Sprintf("对端 %d", index+1))
		ui.PrintInfoCard("对端配置", wireGuardPeerFields(peer)...)
		fmt.Println()
		ui.MenuOption("1", "编辑对端")
		ui.MenuOption("2", "删除对端")
		ui.MenuExit("0/q", "返回对端列表")
		choice, err := view.Ask("请选择：")
		if err != nil {
			return err
		}
		switch {
		case shared.IsReturnChoice(choice):
			return shared.ErrReturnToMenu
		case choice == "1":
			err := editWireGuardPeer(view, name, before, config, index)
			if !errors.Is(err, shared.ErrReturnToMenu) {
				return err
			}
		case choice == "2":
			err := removeWireGuardPeer(view, name, before, config, index)
			if !errors.Is(err, shared.ErrReturnToMenu) {
				return err
			}
		default:
			ui.InvalidChoice()
			view.Pause()
		}
	}
}

func wireGuardPeerValue(peer []wireGuardSetting, key string) string {
	if key != "AllowedIPs" {
		return wireGuardValue(peer, strings.ToLower(key))
	}
	var addresses []string
	for _, setting := range peer {
		if setting.key == "allowedips" && setting.value != "" {
			addresses = append(addresses, setting.value)
		}
	}
	return strings.Join(addresses, ", ")
}

func editWireGuardPeer(view *ui.UI, name string, before []byte, config wireGuardConfig, index int) error {
	if !isRoot() {
		return fmt.Errorf("写入 WireGuard 配置需要 root 权限")
	}
	peer := config.peers[index]
	changes := make(map[string]string)
	notice := ""
	for {
		ui.ClearScreen()
		ui.MenuTitle("WireGuard", name, "管理对端", fmt.Sprintf("编辑对端 %d", index+1))
		if notice != "" {
			ui.PrintField("提示", notice)
			notice = ""
		}
		for i, field := range wireGuardPeerOptions {
			value, changed := changes[field[0]]
			if !changed {
				value = wireGuardPeerValue(peer, field[0])
			}
			switch {
			case field[0] == "PersistentKeepalive" && value == "":
				value = "未设置（默认关闭）"
			case field[0] == "PersistentKeepalive" && (value == "0" || strings.EqualFold(value, "off")):
				value = "0（关闭）"
			case value == "":
				value = "未设置"
			case field[0] == "PresharedKey":
				value = "已配置"
			}
			ui.MenuOptionHint(strconv.Itoa(i+1), field[1], value)
		}
		if len(changes) > 0 {
			ui.MenuShortcutHint("s", "保存对端修改", "")
		}
		ui.MenuExit("0/q", "返回对端详情")
		choice, err := view.Ask("请选择：")
		if err != nil {
			return err
		}
		if shared.IsReturnChoice(choice) {
			if len(changes) == 0 {
				return shared.ErrReturnToMenu
			}
			err := confirmWireGuard(view, "放弃修改并返回")
			if errors.Is(err, shared.ErrReturnToMenu) {
				continue
			}
			if err != nil {
				return err
			}
			return shared.ErrReturnToMenu
		}
		if strings.EqualFold(choice, "s") && len(changes) > 0 {
			updated := updateWireGuardPeer(before, config, index, changes)
			ui.ClearScreen()
			ui.MenuTitle("WireGuard", name, "管理对端", "保存对端修改")
			err := saveWireGuardProfile(view, name, before, updated,
				ui.CardField{Label: "操作", Value: fmt.Sprintf("编辑对端 %d", index+1)})
			if errors.Is(err, shared.ErrReturnToMenu) {
				continue
			}
			return err
		}
		number, err := strconv.Atoi(choice)
		if err != nil || number < 1 || number > len(wireGuardPeerOptions) {
			notice = "请选择列表中的操作"
			continue
		}
		field := wireGuardPeerOptions[number-1]
		value, err := askWireGuardPeerField(view, field[0], field[1])
		if errors.Is(err, shared.ErrReturnToMenu) {
			continue
		}
		if err != nil {
			notice = err.Error()
			continue
		}
		if field[0] == "PublicKey" && value == wireGuardPublicKey(config.device) {
			notice = "对端公钥不能与本机公钥相同"
			continue
		}
		if value == wireGuardPeerValue(peer, field[0]) {
			delete(changes, field[0])
		} else {
			changes[field[0]] = value
		}
	}
}

func askWireGuardPeerField(view *ui.UI, key, label string) (string, error) {
	hint := "回车保留，- 清除，0/q 返回"
	switch key {
	case "PublicKey":
		hint = "回车保留，0/q 返回"
	case "PresharedKey":
		hint = "回车保留，g 生成，- 清除，0/q 返回"
	case "PersistentKeepalive":
		hint = "回车保留，0 关闭，- 清除，q 返回"
	}
	value, err := view.Ask("请输入" + label + "（" + hint + "）：")
	if err != nil {
		return "", err
	}
	if key == "PersistentKeepalive" && (value == defaultWireGuardKeepalive || strings.EqualFold(value, "off")) {
		return defaultWireGuardKeepalive, nil
	}
	if value == "" || shared.IsReturnChoice(value) {
		return "", shared.ErrReturnToMenu
	}
	if value == "-" && key != "PublicKey" {
		return "", nil
	}
	switch key {
	case "PresharedKey":
		if strings.EqualFold(value, "g") {
			return generateWireGuardKey("genpsk", "预共享密钥")
		}
		fallthrough
	case "PublicKey":
		if _, valid := wireGuardKey(value); !valid {
			return "", fmt.Errorf("%s格式无效", label)
		}
	case "AllowedIPs":
		if err := validateWireGuardAllowedIPs(value); err != nil {
			return "", err
		}
	case "Endpoint":
		if !validWireGuardEndpoint(value) {
			return "", fmt.Errorf("连接端点须使用主机:端口，IPv6 地址须加方括号")
		}
	case "PersistentKeepalive":
		if _, err := strconv.ParseUint(value, 10, 16); err != nil {
			return "", fmt.Errorf("保活秒数须在 0–65535 之间，0 表示关闭")
		}
	}
	return value, nil
}

func wireGuardPeerRange(config wireGuardConfig, index, lineCount int) (int, int) {
	start, end := config.peerLines[index], lineCount
	if index+1 < len(config.peerLines) {
		end = config.peerLines[index+1]
	}
	return start, end
}

func updateWireGuardPeer(before []byte, config wireGuardConfig, index int, changes map[string]string) []byte {
	lines := strings.Split(string(before), "\n")
	start, end := wireGuardPeerRange(config, index, len(lines))
	_, cr := strings.CutSuffix(lines[start], "\r")
	newLines := append([]string(nil), lines[:start+1]...)
	replaced := make(map[int]bool)
	for _, field := range wireGuardPeerOptions {
		value, changed := changes[field[0]]
		if !changed {
			continue
		}
		for _, setting := range config.peers[index] {
			if setting.key == strings.ToLower(field[0]) {
				replaced[setting.line] = true
			}
		}
		if value != "" {
			line := field[0] + " = " + value
			if cr {
				line += "\r"
			}
			newLines = append(newLines, line)
		}
	}
	for i := start + 1; i < end; i++ {
		if !replaced[i] {
			newLines = append(newLines, lines[i])
		} else if _, comment, found := strings.Cut(lines[i], "#"); found {
			newLines = append(newLines, "#"+comment)
		}
	}
	return []byte(strings.Join(append(newLines, lines[end:]...), "\n"))
}

func removeWireGuardPeer(view *ui.UI, name string, before []byte, config wireGuardConfig, index int) error {
	if !isRoot() {
		return fmt.Errorf("写入 WireGuard 配置需要 root 权限")
	}
	lines := strings.Split(string(before), "\n")
	start, end := wireGuardPeerRange(config, index, len(lines))
	newLines := append([]string(nil), lines[:start]...)
	for _, line := range lines[start+1 : end] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			newLines = append(newLines, line)
		}
	}
	ui.ClearScreen()
	ui.MenuTitle("WireGuard", name, "管理对端", "删除对端")
	ui.PrintWarningCard(fmt.Sprintf("删除对端 %d", index+1), wireGuardPeerFields(config.peers[index])...)
	return saveWireGuardProfile(view, name, before, []byte(strings.Join(append(newLines, lines[end:]...), "\n")))
}
