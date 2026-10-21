package firewall

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"snail_tool/internal/ui"
)

type liveRules struct {
	version          string
	tables           int
	chains           int
	rules            int
	snailLoaded      bool
	snailDormant     bool
	snailChains      int
	snailRules       int
	inputActive      bool
	otherInputChains int
	inputPolicy      string
	err              error
	json             string
}

type nftObject struct {
	Meta *struct {
		Version string `json:"version"`
	} `json:"metainfo"`
	Table *struct {
		Family string   `json:"family"`
		Name   string   `json:"name"`
		Flags  []string `json:"flags"`
	} `json:"table"`
	Chain *struct {
		Family string `json:"family"`
		Table  string `json:"table"`
		Name   string `json:"name"`
		Type   string `json:"type"`
		Hook   string `json:"hook"`
		Policy string `json:"policy"`
	} `json:"chain"`
	Rule *struct {
		Family string `json:"family"`
		Table  string `json:"table"`
	} `json:"rule"`
}

func inspectLiveRules() liveRules {
	out, err := commandOutput("nft", "-j", "list", "ruleset")
	if err != nil {
		return liveRules{err: fmt.Errorf("%w: %s", err, strings.Join(strings.Fields(out), " "))}
	}
	return parseLiveRules(out)
}

func parseLiveRules(output string) liveRules {
	var document struct {
		Objects []nftObject `json:"nftables"`
	}
	if err := json.Unmarshal([]byte(output), &document); err != nil {
		return liveRules{err: fmt.Errorf("无法解析 nftables 状态: %w", err)}
	}
	if document.Objects == nil {
		return liveRules{err: fmt.Errorf("nftables 状态缺少规则集数据")}
	}
	result := liveRules{json: output}
	var policies []string
	for _, object := range document.Objects {
		if object.Meta != nil {
			result.version = object.Meta.Version
		}
		if object.Table != nil {
			result.tables++
			if object.Table.Family == "inet" && object.Table.Name == "snail" {
				result.snailLoaded = true
				for _, flag := range object.Table.Flags {
					if flag == "dormant" {
						result.snailDormant = true
					}
				}
			}
		}
		if object.Chain != nil {
			result.chains++
			chain := object.Chain
			owned := chain.Family == "inet" && chain.Table == "snail"
			if owned {
				result.snailChains++
			}
			if chain.Type != "filter" || chain.Hook != "input" {
				continue
			}
			if owned {
				result.inputActive = true
				policy := chain.Policy
				switch policy {
				case "drop":
					policy = "默认丢弃（drop）"
				case "accept":
					policy = "默认放行（accept）"
				case "":
					policy = "未返回默认策略"
				}
				policies = append(policies, chain.Name+"："+policy)
			} else {
				result.otherInputChains++
			}
		}
		if object.Rule != nil {
			result.rules++
			if object.Rule.Family == "inet" && object.Rule.Table == "snail" {
				result.snailRules++
			}
		}
	}
	result.inputActive = result.inputActive && result.snailLoaded && !result.snailDormant
	sort.Strings(policies)
	result.inputPolicy = strings.Join(policies, "；")
	return result
}

func currentConfigFields(autostart autostartStatus) []ui.CardField {
	installed := commandExists("nft")
	live := liveRules{}
	if installed {
		live = inspectLiveRules()
	}
	software := ui.InstallationBadge(installed)
	if live.version != "" {
		software += " v" + strings.TrimPrefix(live.version, "v")
	}
	fields := []ui.CardField{
		firewallStateField(installed, live),
		{Label: "开机自启", Value: autostart.label},
	}
	if installed && live.err == nil {
		if live.inputActive {
			fields = append(fields, ui.CardField{Label: "入站策略", Value: strings.TrimPrefix(live.inputPolicy, "input：")})
		}
		if live.snailLoaded {
			fields = append(fields, ui.CardField{Label: "本工具规则", Value: fmt.Sprintf("%d 条链 · %d 条规则", live.snailChains, live.snailRules)})
		}
		if !live.inputActive && live.otherInputChains > 0 {
			fields = append(fields, ui.CardField{Label: "其他入站链", Value: fmt.Sprintf("%d 条，选择 2 查看全部规则", live.otherInputChains)})
		}
	}
	data, fileErr := os.ReadFile(configPath)
	sync := configSyncField(data, fileErr, installed, live)
	state, _, _ := strings.Cut(sync.Value, " · ")
	if state == "已同步" || state == "未同步" {
		sync.Value, sync.Detail = state, ""
	}
	return append(fields, sync, ui.CardField{Label: "nftables", Value: software})
}

func firewallStateField(installed bool, live liveRules) ui.CardField {
	field := ui.CardField{Label: "防火墙"}
	switch {
	case !installed:
		field.Value = "无法检测（未安装 nftables）"
	case live.err != nil:
		field.Value, field.Detail = "检测失败", live.err.Error()
	case live.inputActive:
		field.Value = "已开启（本工具入站规则）"
	case live.snailDormant:
		field.Value = "未开启（本工具表已停用）"
	case live.snailLoaded:
		field.Value = "未开启（入站链未挂接）"
	default:
		field.Value = "未开启（本工具）"
	}
	return field
}
