package firewall

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
)

const (
	sshPortsBegin = "# ===== BEGIN SNAIL SSH PORTS ====="
	sshPortsEnd   = "# ===== END SNAIL SSH PORTS ====="
	sshPortsTag   = "snail:ssh-ports"
)

// EnsureSSHPorts runs before SSH reload. It only changes this tool's active
// input chain, leaving other tables and pending edits in the file unloaded.
// Existing listeners are retained while all newly configured ports are added.
func EnsureSSHPorts(configured []int) (bool, error) {
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
	updated, err := withSSHPorts(string(version.content), ports)
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
	log.Info("防火墙已自动放行 SSH 端口：", portList(ports), "（TCP / UDP、IPv4 / IPv6，保留当前监听端口）")
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

func withSSHPorts(content string, ports []int) (string, error) {
	open, close, insertion, err := inputChainBounds(content)
	if err != nil {
		return "", err
	}
	if _, found, err := readPingSetting(content); err != nil {
		return "", err
	} else if found {
		insertion = strings.Index(content, pingEnd) + len(pingEnd)
	}
	counters, err := packetCountersEnabled(content)
	if err != nil {
		return "", err
	}
	block := sshPortsBegin + "\n        " + strings.Join(sshPortRules(ports, counters), "\n        ") + "\n        " + sshPortsEnd
	begin, end := strings.Index(content, sshPortsBegin), strings.Index(content, sshPortsEnd)
	if begin >= 0 || end >= 0 {
		if strings.Count(content, sshPortsBegin) != 1 || strings.Count(content, sshPortsEnd) != 1 || begin < open || end < begin || end+len(sshPortsEnd) > close {
			return "", fmt.Errorf("SSH 放行区块标记损坏，未修改防火墙配置")
		}
		prefix, err := configTokens(content[insertion:begin])
		if err != nil || len(prefix) > 0 {
			return "", fmt.Errorf("SSH 放行区块前存在其他规则，请先将该区块移到 input 链规则最前面（Ping 区块之后）")
		}
		return content[:begin] + block + content[end+len(sshPortsEnd):], nil
	}
	return content[:insertion] + "\n\n        " + block + content[insertion:], nil
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
