package packages

import (
	"bytes"
	"crypto/ecdh"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"snail_tool/internal/shared"
	"snail_tool/internal/tempguard"
	"snail_tool/internal/ui"
)

var (
	wireGuardConfigDir = "/etc/wireguard"
	wireGuardDataDir   = shared.DataDir
	wireGuardName      = regexp.MustCompile(`^[A-Za-z0-9_=+.-]{1,15}$`)
)

const (
	defaultWireGuardIPv4Prefix = "10.77.0."
	defaultWireGuardAddress    = defaultWireGuardIPv4Prefix + "1/24"
)

func validWireGuardName(name string) bool {
	return wireGuardName.MatchString(name) && name != "." && name != ".."
}

func wireGuardProfilePath(name string) string {
	return filepath.Join(wireGuardConfigDir, name+".conf")
}

func readWireGuardProfile(name string) ([]byte, error) {
	data, exists, err := shared.ReadRegularFile(wireGuardProfilePath(name))
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("未找到 WireGuard 配置 %s", name)
	}
	return data, nil
}

func wireGuardKey(raw string) ([]byte, bool) {
	key, err := base64.StdEncoding.DecodeString(raw)
	return key, err == nil && len(key) == 32 && base64.StdEncoding.EncodeToString(key) == raw
}

type wireGuardSetting struct {
	key, value string
	line       int
}

type wireGuardConfig struct {
	device    []wireGuardSetting
	peers     [][]wireGuardSetting
	peerLines []int
}

func parseWireGuardConfig(data []byte) (wireGuardConfig, error) {
	var config wireGuardConfig
	inInterface := false
	for i, line := range strings.Split(string(data), "\n") {
		line, _, _ = strings.Cut(line, "#")
		line = strings.TrimSpace(line)
		switch strings.ToLower(line) {
		case "":
			continue
		case "[interface]":
			if inInterface {
				return config, fmt.Errorf("第 %d 行：配置只能有一个 [Interface] 区块", i+1)
			}
			inInterface = true
		case "[peer]":
			if !inInterface {
				return config, fmt.Errorf("第 %d 行：[Interface] 必须位于 [Peer] 前", i+1)
			}
			config.peers = append(config.peers, nil)
			config.peerLines = append(config.peerLines, i)
		default:
			key, value, ok := strings.Cut(line, "=")
			key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
			if !inInterface || !ok || key == "" {
				return config, fmt.Errorf("第 %d 行：应在区块内填写字段名和字段值", i+1)
			}
			switch key {
			case "publickey", "privatekey", "presharedkey":
				if _, valid := wireGuardKey(value); !valid {
					return config, fmt.Errorf("第 %d 行：WireGuard 密钥格式无效", i+1)
				}
			}
			setting := wireGuardSetting{key: key, value: value, line: i}
			if len(config.peers) == 0 {
				config.device = append(config.device, setting)
			} else {
				config.peers[len(config.peers)-1] = append(config.peers[len(config.peers)-1], setting)
			}
		}
	}
	if !inInterface {
		return config, fmt.Errorf("配置缺少 [Interface] 区块")
	}
	keys := make(map[string]bool)
	for i, peer := range config.peers {
		key := wireGuardValue(peer, "publickey")
		if key == "" || keys[key] {
			return config, fmt.Errorf("第 %d 个对端缺少公钥或公钥重复", i+1)
		}
		keys[key] = true
	}
	return config, nil
}

func wireGuardValue(settings []wireGuardSetting, key string) string {
	var value string
	for _, setting := range settings {
		if setting.key == key {
			value = setting.value
		}
	}
	return value
}

func wireGuardPublicKey(settings []wireGuardSetting) string {
	key, valid := wireGuardKey(wireGuardValue(settings, "privatekey"))
	if !valid {
		return ""
	}
	private, err := ecdh.X25519().NewPrivateKey(key)
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(private.PublicKey().Bytes())
}

func showWireGuardProfile(name string) error {
	ui.ClearScreen()
	ui.MenuTitle("WireGuard", name, "查看配置")
	data, err := readWireGuardProfile(name)
	if err != nil {
		return err
	}
	config, err := parseWireGuardConfig(data)
	if err != nil {
		return err
	}
	fields := []ui.CardField{{Label: "配置文件", Value: wireGuardProfilePath(name)}}
	if public := wireGuardPublicKey(config.device); public != "" {
		fields = append(fields, ui.CardField{Label: "本机公钥", Value: public})
	}
	fields = append(fields, wireGuardFields(config.device, [][2]string{
		{"address", "隧道地址"}, {"listenport", "监听端口"}, {"dns", "DNS"}, {"mtu", "MTU"},
		{"table", "路由表"}, {"saveconfig", "停止时写回配置"},
	})...)
	ui.PrintInfoCard("接口配置", fields...)
	for i, peer := range config.peers {
		ui.PrintSectionCard(fmt.Sprintf("对端 %d", i+1), wireGuardPeerFields(peer)...)
	}
	return nil
}

func wireGuardFields(settings []wireGuardSetting, labels [][2]string) []ui.CardField {
	var fields []ui.CardField
	for _, label := range labels {
		for _, setting := range settings {
			if setting.key == label[0] {
				fields = append(fields, ui.CardField{Label: label[1], Value: setting.value})
			}
		}
	}
	return fields
}

func createWireGuardProfile(view *ui.UI) error {
	if !isRoot() {
		return fmt.Errorf("写入 WireGuard 配置需要 root 权限")
	}
	name, err := view.Ask("请输入接口名（回车使用 wg0，0/q 返回）：")
	if err != nil {
		return err
	}
	if shared.IsReturnChoice(name) {
		return shared.ErrReturnToMenu
	}
	useDefaults := name == ""
	if useDefaults {
		name = "wg0"
	}
	if !validWireGuardName(name) {
		return fmt.Errorf("接口名无效，支持 1–15 个字母、数字及 _ = + . -")
	}
	path := wireGuardProfilePath(name)
	if _, exists, err := shared.ReadRegularFile(path); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("配置 %s 已存在，请通过编辑入口修改", path)
	}

	address := defaultWireGuardAddress
	if useDefaults {
		address, err = askWireGuardAddress(view)
		if err != nil {
			return err
		}
	}
	key, err := generateWireGuardKey("genkey", "私钥")
	if err != nil {
		return err
	}
	port := 20000 + rand.IntN(60000-20000+1)
	data := []byte(fmt.Sprintf("[Interface]\nPrivateKey = %s\nAddress = %s\nListenPort = %d\n", key, address, port))
	if useDefaults {
		peer, err := askFirstWireGuardPeer(view)
		if err != nil {
			return err
		}
		data = append(data, peer...)
	} else {
		data = append(data, []byte("\n# [Peer]\n# PublicKey = 对端公钥\n# AllowedIPs = 10.77.0.2/32\n# 留空时保存前生成 PresharedKey\n")...)
		data, err = editWireGuardCopy(data)
		if err != nil {
			return err
		}
		data, err = withDefaultWireGuardPSKs(data)
		if err != nil {
			return err
		}
	}
	return saveWireGuardProfile(view, name, nil, data)
}

func parseWireGuardAddress(value string) (netip.Prefix, error) {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "/") {
		return netip.ParsePrefix(value)
	}
	addr, err := netip.ParseAddr(value)
	if err != nil || addr.Zone() != "" {
		return netip.Prefix{}, fmt.Errorf("接口地址格式无效")
	}
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

func askWireGuardAddress(view *ui.UI) (string, error) {
	for {
		value, err := view.Ask("请输入隧道地址（支持尾号 1–255，回车默认 " + defaultWireGuardAddress + "，0/q 返回）：")
		if err != nil {
			return "", err
		}
		if shared.IsReturnChoice(value) {
			return "", shared.ErrReturnToMenu
		}
		if value == "" {
			return defaultWireGuardAddress, nil
		}
		if octet, err := strconv.ParseUint(value, 10, 8); err == nil && octet > 0 {
			return fmt.Sprintf("%s%d/24", defaultWireGuardIPv4Prefix, octet), nil
		}
		addresses := strings.Split(value, ",")
		valid := true
		for i, value := range addresses {
			prefix, err := parseWireGuardAddress(value)
			if err != nil {
				valid = false
				break
			}
			addresses[i] = prefix.String()
		}
		if valid {
			return strings.Join(addresses, ", "), nil
		}
		ui.PrintField("提示", "请输入尾号 1–255，或填写完整的 IPv4 / IPv6 地址。多个完整地址用逗号分隔")
	}
}

func editWireGuardProfile(view *ui.UI, name string) error {
	if !isRoot() {
		return fmt.Errorf("写入 WireGuard 配置需要 root 权限")
	}
	before, err := readWireGuardProfile(name)
	if err != nil {
		return err
	}
	after, err := editWireGuardCopy(before)
	if err != nil {
		return err
	}
	if bytes.Equal(before, after) {
		return shared.ErrReturnToMenu
	}
	return saveWireGuardProfile(view, name, before, after)
}

func editWireGuardCopy(data []byte) ([]byte, error) {
	editor := ""
	for _, name := range []string{"vim", "vi", "nano"} {
		if commandExists(name) {
			editor = name
			break
		}
	}
	if editor == "" {
		return nil, fmt.Errorf("未找到 vim、vi 或 nano")
	}
	release, err := tempguard.Use()
	if err != nil {
		return nil, err
	}
	defer release()
	dir := filepath.Join(wireGuardDataDir, shared.DataTempDirName)
	if err := shared.EnsurePrivateDir(wireGuardDataDir); err != nil {
		return nil, err
	}
	if err := shared.EnsurePrivateDir(dir); err != nil {
		return nil, err
	}
	copy, err := os.CreateTemp(dir, ".wireguard-edit-*.conf")
	if err != nil {
		return nil, err
	}
	path := copy.Name()
	defer os.Remove(path)
	_, writeErr := copy.Write(data)
	closeErr := copy.Close()
	if writeErr != nil {
		return nil, writeErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	args := []string{path}
	if editor == "vim" {
		args = []string{"-n", "-i", "NONE", "-c", "set nobackup nowritebackup", path}
	}
	if err := commandRun(editor, args...); err != nil {
		return nil, fmt.Errorf("编辑 WireGuard 配置失败: %w", err)
	}
	edited, exists, err := shared.ReadRegularFile(path)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("编辑副本已被删除")
	}
	return edited, nil
}

func confirmWireGuard(view *ui.UI, prompt string) error {
	ok, err := view.Confirm(prompt + "？(y/N，0/q 返回)：")
	if err == nil && !ok {
		return shared.ErrReturnToMenu
	}
	return err
}

func saveWireGuardProfile(view *ui.UI, name string, before, after []byte, details ...ui.CardField) error {
	config, err := parseWireGuardConfig(after)
	if err != nil {
		return err
	}
	path := wireGuardProfilePath(name)
	checkRuntime := before != nil && commandExists("wg")
	var unit wireGuardUnit
	var running, saveOnStop bool
	if checkRuntime {
		active, err := wireGuardActiveNames()
		if err != nil {
			return err
		}
		running = active[name]
		if running {
			unit = inspectWireGuardUnit(name)
			if !unit.active && !commandExists("wg-quick") {
				return fmt.Errorf("缺少 wg-quick，无法在修改后重启接口")
			}
			old, err := parseWireGuardConfig(before)
			if err != nil {
				return fmt.Errorf("无法读取当前运行接口的配置，请停止接口后修正: %w", err)
			}
			saveOnStop = strings.EqualFold(wireGuardValue(old.device, "saveconfig"), "true")
		}
	}
	effect := "下次启动接口时读取"
	if running {
		effect = "确认后重启接口，连接会短暂中断"
	}
	fields := []ui.CardField{
		{Label: "路径", Value: path},
		{Label: "对端数量", Value: strconv.Itoa(len(config.peers))},
		{Label: "生效方式", Value: effect},
	}
	fields = append(fields, details...)
	fields = append(fields, wireGuardFields(config.device, [][2]string{
		{"address", "隧道地址"}, {"listenport", "监听端口"},
	})...)
	for _, peer := range config.peers {
		if wireGuardValue(peer, "presharedkey") != "" {
			fields = append(fields, ui.CardField{Label: "预共享密钥", Value: "对应的两端必须填写相同值"})
			break
		}
	}
	if strings.EqualFold(wireGuardValue(config.device, "saveconfig"), "true") {
		fields = append(fields, ui.CardField{Label: "SaveConfig", Value: "接口停止时将运行状态写回配置文件"})
	}
	ui.PrintInfoCard("保存 WireGuard 配置", fields...)
	prompt := "确认保存"
	if running {
		prompt = "确认保存并重启接口"
	}
	if err := confirmWireGuard(view, prompt); err != nil {
		return err
	}
	if err := checkWireGuardProfile(path, before); err != nil {
		return err
	}
	if checkRuntime {
		active, err := wireGuardActiveNames()
		if err != nil {
			return err
		}
		if active[name] != running || running && inspectWireGuardUnit(name) != unit {
			return fmt.Errorf("接口或服务状态已变化，请返回后重试")
		}
	}
	if before != nil {
		if err := backupWireGuardProfile(path); err != nil {
			return err
		}
	}
	if err := shared.EnsurePrivateDir(wireGuardConfigDir); err != nil {
		return err
	}
	if running {
		if err := restartWireGuardWithProfile(name, before, after, unit.active, saveOnStop); err != nil {
			return err
		}
		ui.PrintSuccessCard("WireGuard 配置已保存，接口已重启")
		return nil
	}
	if err := writeWireGuardProfile(path, before, after); err != nil {
		return err
	}
	ui.PrintSuccessCard("WireGuard 配置已保存")
	return nil
}

func writeWireGuardProfile(path string, before, after []byte) error {
	return shared.AtomicWrite(path, shared.AtomicWriteOptions{Mode: 0600, ForceMode: true}, func(w io.Writer) error {
		if err := checkWireGuardProfile(path, before); err != nil {
			return err
		}
		_, err := w.Write(after)
		return err
	})
}

func restartWireGuardWithProfile(name string, before, after []byte, useSystemd, saveOnStop bool) error {
	path := wireGuardProfilePath(name)
	if err := checkWireGuardProfile(path, before); err != nil {
		return err
	}
	// Stop with the previous settings so wg-quick removes their routes and
	// SaveConfig writes back before the new configuration is installed.
	if err := applyWireGuardRuntime(name, "stop", useSystemd); err != nil {
		return fmt.Errorf("停止接口失败，未保存新配置: %w", err)
	}
	stopped := before
	if saveOnStop {
		var err error
		stopped, err = readWireGuardProfile(name)
		if err != nil {
			return fmt.Errorf("接口已停止，读取写回的旧配置失败，未保存新配置: %w", err)
		}
	}
	if err := writeWireGuardProfile(path, stopped, after); err != nil {
		if checkWireGuardProfile(path, stopped) == nil {
			if startErr := applyWireGuardRuntime(name, "start", useSystemd); startErr == nil {
				return fmt.Errorf("保存新配置失败，已重新启动旧接口配置: %w", err)
			}
		}
		return fmt.Errorf("保存新配置失败，请检查接口状态: %w", err)
	}
	if err := applyWireGuardRuntime(name, "start", useSystemd); err != nil {
		active, statusErr := wireGuardActiveNames()
		if statusErr != nil || active[name] {
			return fmt.Errorf("新配置已保存，接口启动结果异常，请查看运行状态: %w", err)
		}
		if restoreErr := writeWireGuardProfile(path, after, before); restoreErr != nil {
			return fmt.Errorf("新配置启动失败且旧配置未恢复: %w", errors.Join(err, restoreErr))
		}
		if startErr := applyWireGuardRuntime(name, "start", useSystemd); startErr != nil {
			return fmt.Errorf("新配置启动失败，已恢复旧配置文件，但旧接口也未能启动: %w", errors.Join(err, startErr))
		}
		return fmt.Errorf("新配置启动失败，已恢复并启动旧配置: %w", err)
	}
	return nil
}

func checkWireGuardProfile(path string, before []byte) error {
	latest, exists, err := shared.ReadRegularFile(path)
	if err != nil {
		return err
	}
	if exists != (before != nil) || !bytes.Equal(before, latest) {
		return fmt.Errorf("配置文件已变化，请返回后重新操作")
	}
	return nil
}

func backupWireGuardProfile(path string) error {
	_, err := (shared.BackupStore{Root: wireGuardDataDir, Module: "wireguard"}).Snapshot(path)
	return err
}

func saveWireGuardPeer(view *ui.UI, name string, before []byte, peer string) error {
	after := append([]byte(nil), bytes.TrimRight(before, "\r\n")...)
	return saveWireGuardProfile(view, name, before, append(after, peer...))
}

func generateWireGuardKey(command, label string) (string, error) {
	key, err := commandOutput("wg", command)
	key = strings.TrimSpace(key)
	if _, valid := wireGuardKey(key); err != nil || !valid {
		return "", fmt.Errorf("生成 WireGuard %s失败", label)
	}
	return key, nil
}

func withDefaultWireGuardPSKs(data []byte) ([]byte, error) {
	config, err := parseWireGuardConfig(data)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	for index, peer := range config.peers {
		if wireGuardValue(peer, "presharedkey") != "" {
			continue
		}
		key, err := generateWireGuardKey("genpsk", "预共享密钥")
		if err != nil {
			return nil, err
		}
		line := config.peerLines[index]
		header, crlf := strings.CutSuffix(lines[line], "\r")
		if crlf {
			lines[line] = header + "\r\nPresharedKey = " + key + "\r"
		} else {
			lines[line] = header + "\nPresharedKey = " + key
		}
	}
	return []byte(strings.Join(lines, "\n")), nil
}

func printWireGuardPeerInputFormat() {
	ui.PrintField("输入格式", "对端公钥 AllowedIPs [Endpoint] [保活秒数]")
	ui.PrintField("说明", "AllowedIPs 用逗号分隔，Endpoint 可填主机名或 IP 加端口。用 - 跳过 Endpoint")
	ui.PrintField("预共享密钥", "默认生成。可粘贴快速配置里的 --psk=密钥，或在末尾加 --no-psk 跳过")
}

func askFirstWireGuardPeer(view *ui.UI) (string, error) {
	printWireGuardPeerInputFormat()
	line, err := view.Ask("请输入首个对端配置（回车跳过，0/q 返回）：")
	if err != nil {
		return "", err
	}
	if shared.IsReturnChoice(line) {
		return "", shared.ErrReturnToMenu
	}
	if line == "" {
		return "", nil
	}
	return wireGuardPeerFromInput(line)
}

func wireGuardPeerFromInput(line string) (string, error) {
	fields := strings.Fields(line)
	generatePSK, psk := true, ""
	if len(fields) > 0 {
		last := fields[len(fields)-1]
		if last == "--no-psk" {
			generatePSK = false
			fields = fields[:len(fields)-1]
		} else if supplied, found := strings.CutPrefix(last, "--psk="); found {
			if _, valid := wireGuardKey(supplied); !valid {
				return "", fmt.Errorf("WireGuard 预共享密钥格式无效")
			}
			generatePSK, psk = false, supplied
			fields = fields[:len(fields)-1]
		}
	}
	if len(fields) < 2 || len(fields) > 4 {
		return "", fmt.Errorf("请填写对端公钥和 AllowedIPs")
	}
	if _, valid := wireGuardKey(fields[0]); !valid {
		return "", fmt.Errorf("对端公钥格式无效")
	}
	if err := validateWireGuardAllowedIPs(fields[1]); err != nil {
		return "", err
	}
	peer := fmt.Sprintf("\n\n[Peer]\nPublicKey = %s\nAllowedIPs = %s\n", fields[0], fields[1])
	if len(fields) >= 3 && fields[2] != "-" {
		endpoint := fields[2]
		if !validWireGuardEndpoint(endpoint) {
			return "", fmt.Errorf("Endpoint 须使用 主机:端口，IPv6 地址须加方括号")
		}
		peer += "Endpoint = " + endpoint + "\n"
	}
	if len(fields) == 4 {
		seconds, err := strconv.ParseUint(fields[3], 10, 16)
		if err != nil {
			return "", fmt.Errorf("保活秒数须在 0–65535 之间")
		}
		peer += fmt.Sprintf("PersistentKeepalive = %d\n", seconds)
	}
	if generatePSK {
		key, err := generateWireGuardKey("genpsk", "预共享密钥")
		if err != nil {
			return "", err
		}
		psk = key
	}
	if psk != "" {
		peer += "PresharedKey = " + psk + "\n"
	}
	return peer, nil
}

func rotateWireGuardKeys(view *ui.UI, name string) error {
	if !isRoot() {
		return fmt.Errorf("更换 WireGuard 密钥需要 root 权限")
	}
	ui.ClearScreen()
	ui.MenuTitle("WireGuard", name, "更换密钥")
	before, err := readWireGuardProfile(name)
	if err != nil {
		return err
	}
	config, err := parseWireGuardConfig(before)
	if err != nil {
		return err
	}
	if wireGuardValue(config.device, "privatekey") == "" {
		return fmt.Errorf("当前配置缺少接口私钥")
	}

	var peersWithPSK [][]wireGuardSetting
	for _, peer := range config.peers {
		if wireGuardValue(peer, "presharedkey") != "" {
			peersWithPSK = append(peersWithPSK, peer)
		}
	}
	if len(peersWithPSK) > 0 {
		rotatePSK, err := view.Confirm("是否同时更换已配置的预共享密钥？(y/N，0/q 返回)：")
		if err != nil {
			return err
		}
		if !rotatePSK {
			peersWithPSK = nil
		}
	}

	lines := strings.Split(string(before), "\n")
	privateKey, err := generateWireGuardKey("genkey", "私钥")
	if err != nil {
		return err
	}
	for _, setting := range config.device {
		if setting.key == "privatekey" {
			lines[setting.line] = strings.Replace(lines[setting.line], setting.value, privateKey, 1)
		}
	}
	for _, peer := range peersWithPSK {
		key, err := generateWireGuardKey("genpsk", "预共享密钥")
		if err != nil {
			return err
		}
		for _, setting := range peer {
			if setting.key == "presharedkey" {
				lines[setting.line] = strings.Replace(lines[setting.line], setting.value, key, 1)
			}
		}
	}

	scope, sync := "本机私钥", "各对端需更新本机公钥"
	if len(peersWithPSK) > 0 {
		scope += fmt.Sprintf("和 %d 个对端的预共享密钥", len(peersWithPSK))
		sync += "，相关对端还需更新对应的预共享密钥"
	}
	return saveWireGuardProfile(view, name, before, []byte(strings.Join(lines, "\n")),
		ui.CardField{Label: "更换范围", Value: scope},
		ui.CardField{Label: "对端同步", Value: sync},
	)
}

func validWireGuardEndpoint(endpoint string) bool {
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil || host == "" || strings.ContainsAny(host, "#=/ \t\r\n") {
		return false
	}
	value, err := strconv.ParseUint(port, 10, 16)
	return err == nil && value > 0
}

func validateWireGuardAllowedIPs(value string) error {
	for _, prefix := range strings.Split(value, ",") {
		if _, err := netip.ParsePrefix(strings.TrimSpace(prefix)); err != nil {
			return fmt.Errorf("AllowedIPs 须使用 CIDR，多个网段用逗号分隔")
		}
	}
	return nil
}
