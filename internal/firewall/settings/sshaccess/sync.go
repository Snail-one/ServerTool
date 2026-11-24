package sshaccess

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/firewall/loadedrules"
	"snail_tool/internal/firewall/settings/counters"
	"snail_tool/internal/firewall/settings/ping"
	"snail_tool/internal/log"
	"snail_tool/internal/storage"
	"snail_tool/internal/tempguard"
)

const (
	LegacyBegin = "# ===== BEGIN SNAIL SSH PORTS ====="
	LegacyEnd   = "# ===== END SNAIL SSH PORTS ====="
	RuleTag     = "snail:ssh-ports"
)

// EnsureSSHPorts runs before SSH reload. It only changes this tool's active
// input chain, leaving other tables and pending edits in the file unloaded.
// Definitions persist the configured ports; current listeners are also allowed
// in the live transaction so switching SSH ports keeps the current connection reachable.
func (service Service) EnsureSSHPorts(configured []int) (bool, error) {
	release, err := tempguard.Use()
	if err != nil {
		return false, err
	}
	defer release()

	if !service.CommandExists("nft") {
		return false, nil
	}
	live := loadedrules.Inspect(service.Store.CommandOutput)
	if live.Err != nil {
		return false, fmt.Errorf("无法检查防火墙，未重载 SSH: %w", live.Err)
	}
	if !live.SnailLoaded {
		return false, nil
	}
	handles, err := sshRuleHandles(live.JSON)
	if err != nil {
		return false, err
	}
	listening, err := service.DetectListeningPorts()
	if err != nil {
		return false, fmt.Errorf("无法保留当前 SSH 监听端口，未重载 SSH: %w", err)
	}
	ports, err := MergePorts(configured, listening)
	if err != nil {
		return false, err
	}
	version, err := service.Store.Read()
	if err != nil {
		return false, err
	}
	if !version.Exists {
		return false, fmt.Errorf("防火墙已开启，但缺少 %s，无法持久保存 SSH 放行规则，未重载 SSH", service.Store.Path)
	}
	configured, err = MergePorts(configured)
	if err != nil {
		return false, err
	}
	updated, err := ReplacePorts(string(version.Content), configured)
	if err != nil {
		return false, err
	}

	// Apply a small transaction instead of reloading the whole file: unrelated
	// live rules survive, and pending manual edits do not become effective here.
	var batch strings.Builder
	for _, handle := range handles {
		fmt.Fprintf(&batch, "delete rule inet snail input handle %d\n", handle)
	}
	counters, err := counters.Enabled(string(version.Content))
	if err != nil {
		return false, err
	}
	rules := sshPortRules(ports, counters)
	// Each insert prepends a rule. Reverse the commands so the resulting
	// chain order agrees with the persistent block (TCP followed by UDP).
	for i := len(rules) - 1; i >= 0; i-- {
		fmt.Fprintf(&batch, "insert rule inet snail input %s\n", rules[i])
	}
	tmp, err := os.CreateTemp(filepath.Dir(service.Store.Path), ".snail-ssh-ports-*.nft")
	if err != nil {
		return false, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(batch.String()); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	if out, err := service.Store.CommandOutput("nft", "-c", "-f", tmp.Name()); err != nil {
		return false, fmt.Errorf("SSH 放行规则校验失败，未重载 SSH: %w: %s", err, strings.TrimSpace(out))
	}
	changed := !bytes.Equal(version.Content, []byte(updated))
	if changed {
		// Also validate the complete persistent configuration before saving it.
		if err := service.Store.Write(updated, version); err != nil {
			return false, err
		}
	} else if err := service.Store.Check(version); err != nil {
		return false, err
	}
	if err := service.Store.PreserveOriginal(); err != nil {
		return false, err
	}
	if out, err := service.Store.CommandOutput("nft", "-f", tmp.Name()); err != nil {
		loadErr := fmt.Errorf("SSH 放行规则加载失败，未重载 SSH: %w: %s", err, strings.TrimSpace(out))
		if changed {
			if rollbackErr := service.restoreSSHFirewallConfig(updated, version.Content); rollbackErr != nil {
				return false, fmt.Errorf("%w；恢复防火墙配置失败: %v", loadErr, rollbackErr)
			}
		}
		return false, loadErr
	}
	log.Info("防火墙 SSH 端口定义已更新：", PortList(configured))
	log.Info("当前规则已放行：", PortList(ports), "（TCP / UDP、IPv4 / IPv6，保留当前监听端口；重新加载配置后以端口定义为准）")
	return true, nil
}

func MergePorts(groups ...[]int) ([]int, error) {
	seen := make(map[int]bool)
	for _, group := range groups {
		for _, port := range group {
			if port < 1 || port > 65535 {
				return nil, fmt.Errorf("SSH 端口无效: %d", port)
			}
			seen[port] = true
		}
	}
	if len(seen) == 0 {
		return nil, fmt.Errorf("SSH 端口不能为空")
	}
	ports := make([]int, 0, len(seen))
	for port := range seen {
		ports = append(ports, port)
	}
	sort.Ints(ports)
	return ports, nil
}

func sshPortRules(ports []int, counters bool) []string {
	var rules []string
	counter := "counter "
	if !counters {
		counter = ""
	}
	for _, protocol := range []string{"tcp", "udp"} {
		rules = append(rules, fmt.Sprintf("%s dport { %s } %saccept comment %q", protocol, PortList(ports), counter, RuleTag))
	}
	return rules
}

// ReadPorts returns the service ports recorded in set snail_ssh_port.
// The eight input lists store their own elements and can omit these ports.
func ReadPorts(content string) ([]int, error) {
	raw, err := configuration.NamedSetValue(content, "snail_ssh_port")
	if err != nil {
		return nil, fmt.Errorf("SSH 端口集合缺失、重复或格式异常，请检查配置")
	}
	ports, err := configuration.ParsePorts(raw)
	if err != nil || len(ports) == 0 {
		return nil, fmt.Errorf("SSH 端口集合内容无效，请重新生成防火墙配置")
	}
	numbers := make([]int, 0, len(ports))
	for _, port := range ports {
		if port.First != port.Last || port.First < 1 || port.First > 65535 {
			return nil, fmt.Errorf("SSH 端口集合内容无效，请重新生成防火墙配置")
		}
		numbers = append(numbers, port.First)
	}
	return numbers, nil
}

func PortText(content string) (string, error) {
	ports, err := ReadPorts(content)
	if err != nil {
		return "", err
	}
	return PortList(ports), nil
}

// ReplacePorts records the new SSH service ports. Input lists that still
// contain an old SSH port have those ports replaced; lists the user already
// cleared of SSH are left unchanged. Older define formats must be regenerated.
func ReplacePorts(content string, ports []int) (string, error) {
	ports, err := MergePorts(ports)
	if err != nil {
		return "", err
	}
	if strings.Contains(content, LegacyBegin) || strings.Contains(content, LegacyEnd) {
		return "", fmt.Errorf("防火墙配置含旧 SSH 放行区块，请先重新生成防火墙配置")
	}
	if _, _, _, err := configuration.InputChainBounds(content); err != nil {
		return "", err
	}
	if _, _, err := ping.Read(content); err != nil {
		return "", err
	}
	old, err := ReadPorts(content)
	if err != nil {
		return "", err
	}
	for _, list := range inputPortSets() {
		if _, err := configuration.ReadPortSet(content, list); err != nil {
			return "", fmt.Errorf("业务端口列表异常，请检查配置: %w", err)
		}
	}
	updated, err := configuration.ReplaceNamedSetElements(content, "snail_ssh_port", PortList(ports))
	if err != nil {
		return "", err
	}
	oldIntervals, _ := configuration.ParsePorts(PortList(old))
	newIntervals, _ := configuration.ParsePorts(PortList(ports))
	for _, list := range inputPortSets() {
		current, err := configuration.ReadPortSet(updated, list)
		if err != nil {
			return "", err
		}
		parsed, err := configuration.ParsePorts(current)
		if err != nil {
			return "", err
		}
		if len(configuration.IntersectPorts(parsed, oldIntervals)) == 0 {
			continue
		}
		next := configuration.UnionPorts(append(configuration.SubtractPorts(parsed, oldIntervals), newIntervals...))
		updated, err = configuration.ReplaceNamedSetElements(updated, list.Name, configuration.PortsText(next))
		if err != nil {
			return "", err
		}
	}
	return updated, nil
}

func sshRuleHandles(output string) ([]uint64, error) {
	var document struct {
		Objects []struct {
			Chain *struct{ Family, Table, Name, Type, Hook string } `json:"chain"`
			Rule  *struct {
				Family, Table, Chain, Comment string
				Handle                        uint64
			} `json:"rule"`
		} `json:"nftables"`
	}
	if err := json.Unmarshal([]byte(output), &document); err != nil {
		return nil, err
	}
	var handles []uint64
	found := false
	for _, object := range document.Objects {
		if chain := object.Chain; chain != nil && chain.Family == "inet" && chain.Table == "snail" && chain.Name == "input" {
			found = chain.Hook == "input" && chain.Type == "filter"
		}
		if rule := object.Rule; rule != nil && rule.Family == "inet" && rule.Table == "snail" && rule.Chain == "input" && rule.Comment == RuleTag {
			if rule.Handle == 0 {
				return nil, fmt.Errorf("SSH 放行规则缺少句柄，无法安全更新")
			}
			handles = append(handles, rule.Handle)
		}
	}
	if !found {
		return nil, fmt.Errorf("已加载的 inet snail input 链未挂接入站过滤 hook，未重载 SSH")
	}
	return handles, nil
}

func (service Service) restoreSSHFirewallConfig(expected string, original []byte) error {
	if err := service.Store.Check(configuration.Version{Content: []byte(expected), Exists: true}); err != nil {
		return err
	}
	return storage.AtomicWriteFile(service.Store.Path, original, storage.AtomicWriteOptions{Mode: 0644})
}

func inputPortSets() []configuration.PortSet {
	public, private := configuration.InputPortSets(true), configuration.InputPortSets(false)
	return append(public[:], private[:]...)
}

func PortList(ports []int) string {
	values := make([]string, len(ports))
	for i, port := range ports {
		values[i] = strconv.Itoa(port)
	}
	return strings.Join(values, ", ")
}
