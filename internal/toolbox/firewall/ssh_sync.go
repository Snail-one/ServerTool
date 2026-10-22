package firewall

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/tempguard"
)

const (
	sshPortsBegin = "# ===== BEGIN SNAIL SSH PORTS ====="
	sshPortsEnd   = "# ===== END SNAIL SSH PORTS ====="
	sshPortsTag   = "snail:ssh-ports"
)

// EnsureSSHPorts runs before SSH reload. It only changes this tool's active
// input chain, leaving other tables and pending edits in the file unloaded.
// Definitions persist the configured ports; current listeners are also allowed
// in the live transaction so switching SSH ports keeps the current connection reachable.
func EnsureSSHPorts(configured []int) (bool, error) {
	release, err := tempguard.Use()
	if err != nil {
		return false, err
	}
	defer release()

	if !commandExists("nft") {
		return false, nil
	}
	live := inspectLiveRules()
	if live.err != nil {
		return false, fmt.Errorf("无法检查防火墙，未重载 SSH: %w", live.err)
	}
	if !live.snailLoaded {
		return false, nil
	}
	handles, err := sshRuleHandles(live.json)
	if err != nil {
		return false, err
	}
	listening, err := detectListeningSSHPorts()
	if err != nil {
		return false, fmt.Errorf("无法保留当前 SSH 监听端口，未重载 SSH: %w", err)
	}
	ports, err := mergeSSHPorts(configured, listening)
	if err != nil {
		return false, err
	}
	version, err := readConfigVersion()
	if err != nil {
		return false, err
	}
	if !version.exists {
		return false, fmt.Errorf("防火墙已开启，但缺少 %s，无法持久保存 SSH 放行规则，未重载 SSH", configPath)
	}
	configured, err = mergeSSHPorts(configured)
	if err != nil {
		return false, err
	}
	updated, err := withSSHPorts(string(version.content), configured)
	if err != nil {
		return false, err
	}

	// Apply a small transaction instead of reloading the whole file: unrelated
	// live rules survive, and pending manual edits do not become effective here.
	var batch strings.Builder
	for _, handle := range handles {
		fmt.Fprintf(&batch, "delete rule inet snail input handle %d\n", handle)
	}
	counters, err := packetCountersEnabled(string(version.content))
	if err != nil {
		return false, err
	}
	rules := sshPortRules(ports, counters)
	// Each insert prepends a rule. Reverse the commands so the resulting
	// chain order agrees with the persistent block (TCP followed by UDP).
	for i := len(rules) - 1; i >= 0; i-- {
		fmt.Fprintf(&batch, "insert rule inet snail input %s\n", rules[i])
	}
	tmp, err := os.CreateTemp(filepath.Dir(configPath), ".snail-ssh-ports-*.nft")
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
	if out, err := commandOutput("nft", "-c", "-f", tmp.Name()); err != nil {
		return false, fmt.Errorf("SSH 放行规则校验失败，未重载 SSH: %w: %s", err, strings.TrimSpace(out))
	}
	changed := !bytes.Equal(version.content, []byte(updated))
	if changed {
		// Also validate the complete persistent configuration before saving it.
		if err := writeConfig(updated, version); err != nil {
			return false, err
		}
	} else if err := checkConfigVersion(version); err != nil {
		return false, err
	}
	if err := preserveOriginal(); err != nil {
		return false, err
	}
	if out, err := commandOutput("nft", "-f", tmp.Name()); err != nil {
		loadErr := fmt.Errorf("SSH 放行规则加载失败，未重载 SSH: %w: %s", err, strings.TrimSpace(out))
		if changed {
			if rollbackErr := restoreSSHFirewallConfig(updated, version.content); rollbackErr != nil {
				return false, fmt.Errorf("%w；恢复防火墙配置失败: %v", loadErr, rollbackErr)
			}
		}
		return false, loadErr
	}
	log.Info("防火墙 SSH 端口定义已更新：", portList(configured))
	log.Info("当前规则已放行：", portList(ports), "（TCP / UDP、IPv4 / IPv6，保留当前监听端口；重新加载配置后以端口定义为准）")
	return true, nil
}

func mergeSSHPorts(groups ...[]int) ([]int, error) {
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
		rules = append(rules, fmt.Sprintf("%s dport { %s } %saccept comment %q", protocol, portList(ports), counter, sshPortsTag))
	}
	return rules
}

var sshDefinitionLine = regexp.MustCompile(`(?m)^[\t ]*define[\t ]+snail_ssh_port[\t ]*=[\t ]*\{([0-9,\t \r\n]+)\}[\t ]*(?:#[^\n]*)?\r?$`)
var sshPortListLine = regexp.MustCompile(`(?m)^([\t ]*define[\t ]+(snail_(?:public|private)_ipv[46]_(?:tcp|udp)_ports)[\t ]*=[\t ]*\{)([^{}\n]*)(\}[\t ]*(?:#[^\n]*)?\r?)$`)

// withSSHPorts updates only the generated SSH port set. The eight port lists
// retain the same reference regardless of how many SSH ports are configured.
// Older formats must be replaced by regenerating the config.
func withSSHPorts(content string, ports []int) (string, error) {
	ports, err := mergeSSHPorts(ports)
	if err != nil {
		return "", err
	}
	if strings.Contains(content, sshPortsBegin) || strings.Contains(content, sshPortsEnd) {
		return "", fmt.Errorf("防火墙配置含旧 SSH 放行区块，请先重新生成防火墙配置")
	}
	if _, _, _, err := inputChainBounds(content); err != nil {
		return "", err
	}
	if _, _, err := readPingSetting(content); err != nil {
		return "", err
	}
	definitions := sshDefinitionLine.FindAllStringSubmatchIndex(content, -1)
	if len(definitions) != 1 {
		return "", fmt.Errorf("SSH 端口集合定义缺失、重复或格式异常，请重新生成防火墙配置")
	}
	definition := definitions[0]
	for _, value := range strings.Split(content[definition[2]:definition[3]], ",") {
		port, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || port < 1 || port > 65535 {
			return "", fmt.Errorf("SSH 端口集合内容无效，请重新生成防火墙配置")
		}
	}
	lists := make(map[string]bool)
	for _, parts := range sshPortListLine.FindAllStringSubmatch(content, -1) {
		if lists[parts[2]] {
			return "", fmt.Errorf("业务端口列表重复，请重新生成防火墙配置")
		}
		lists[parts[2]] = true
		references := 0
		for _, raw := range strings.Split(parts[3], ",") {
			value := strings.TrimSpace(raw)
			if value == "$snail_ssh_port" {
				references++
			} else if strings.Contains(value, "$snail_ssh_port") {
				return "", fmt.Errorf("SSH 端口集合引用异常，请重新生成防火墙配置")
			}
		}
		if references != 1 {
			return "", fmt.Errorf("业务端口列表缺少唯一的 SSH 集合引用，请重新生成防火墙配置")
		}
	}
	if len(lists) != 8 {
		return "", fmt.Errorf("缺少完整的公网、内网 TCP / UDP 端口列表，请重新生成防火墙配置")
	}
	return content[:definition[2]] + " " + portList(ports) + " " + content[definition[3]:], nil
}

// inputChainBounds returns the body and the end of its base-chain declarations.
func inputChainBounds(content string) (int, int, int, error) {
	tokens, err := configTokens(content)
	if err != nil {
		return 0, 0, 0, err
	}
	open, close, insertion := -1, -1, -1
	for i := 0; i+3 < len(tokens); i++ {
		if tokens[i].text != "table" || tokens[i+1].text != "inet" || tokens[i+2].text != "snail" || tokens[i+3].text != "{" {
			continue
		}
		end, err := closingBrace(tokens, i+3)
		if err != nil {
			return 0, 0, 0, err
		}
		for j := i + 4; j+2 < end; j++ {
			if tokens[j].text != "chain" || tokens[j+2].text != "{" {
				continue
			}
			chainEnd, err := closingBrace(tokens, j+2)
			if err != nil {
				return 0, 0, 0, err
			}
			if tokens[j+1].text == "input" {
				if open >= 0 {
					return 0, 0, 0, fmt.Errorf("发现多个 inet snail input 链，未修改规则")
				}
				open, close = tokens[j+2].end, tokens[chainEnd].start
				if j+6 >= chainEnd || tokens[j+3].text != "type" || tokens[j+4].text != "filter" || tokens[j+5].text != "hook" || tokens[j+6].text != "input" {
					return 0, 0, 0, fmt.Errorf("配置中的 inet snail input 链未挂接入站过滤 hook，未修改规则")
				}
				// Only declarations may precede the insertion point. This avoids
				// persisting an allow rule behind an earlier terminal verdict.
				declarationEnd := j + 3
				for declarationEnd < chainEnd && tokens[declarationEnd].text != ";" {
					declarationEnd++
				}
				for k := j + 3; k+2 < chainEnd; k++ {
					if tokens[k].text == "policy" && (tokens[k+1].text == "drop" || tokens[k+1].text == "accept") && tokens[k+2].text == ";" {
						if k != declarationEnd+1 {
							return 0, 0, 0, fmt.Errorf("input 链声明顺序无法安全识别，未修改规则")
						}
						insertion = tokens[k+2].end
						break
					}
				}
			}
			j = chainEnd
		}
		i = end
	}
	if insertion < 0 {
		return 0, 0, 0, fmt.Errorf("配置中未找到 inet snail input 基础链及默认策略，请先生成防火墙配置")
	}
	return open, close, insertion, nil
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
		if rule := object.Rule; rule != nil && rule.Family == "inet" && rule.Table == "snail" && rule.Chain == "input" && rule.Comment == sshPortsTag {
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

func restoreSSHFirewallConfig(expected string, original []byte) error {
	if err := checkConfigVersion(configVersion{content: []byte(expected), exists: true}); err != nil {
		return err
	}
	return shared.AtomicWriteFile(configPath, original, shared.AtomicWriteOptions{Mode: 0644})
}
